// Copyright 2023 Adam Chalkley
//
// https://github.com/atc0005/go-nagios
//
// Licensed under the MIT License. See LICENSE file in the project root for
// full license information.

package nagios

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// perfDataMinSemicolonSeparatedFields indicates the minimum number of
	// fields from an attempt to split a performance data metric string using
	// semicolon as the separator. If no semicolons are present in the input
	// string the original input string is returned as-is, thus one field. If
	// the input string is empty then 0 fields are returned.
	perfDataMinSemicolonSeparatedFields int = 1

	// perfDataMaxSemicolonSeparatedFields indicates how many fields separated
	// by semicolons are expected/permitted in a performance data metric input
	// string; this value is the number of total permitted semicolons + 1.
	perfDataMaxSemicolonSeparatedFields int = 5

	// perfDataValueFieldRegex represents the regex character class used to
	// validate the Value field. In addition to the characters used to
	// represent whole and fractional numbers a literal U character is also
	// permitted (indicates that the actual value could not be determined).
	perfDataValueFieldRegex string = `[-0-9.]+|U`

	// perfDataMinMaxFieldsRegex represents the regex character class
	// used to validate the Min and Max fields.
	perfDataMinMaxFieldsRegex string = `[-0-9.]+`

	// perfDataThresholdRangeSyntaxRegex represents the regex character class
	// used to validate and parse the Warn and Crit fields.
	perfDataThresholdRangeSyntaxRegex string = `[0-9~@:]+`

	// perfDataLabelFieldDisallowedCharacters are the characters disallowed in
	// the Label field; the equals sign and single quote characters are not
	// allowed.
	perfDataLabelFieldDisallowedCharacters string = `='`

	// perfDataUoMFieldDisallowedCharacters are the characters disallowed in
	// the Unit of Measurement field; numbers, semicolons and quotes are not
	// allowed.
	perfDataUoMFieldDisallowedCharacters string = `0123456789;'"`

	// perfDataValueFieldRegex is the name of the regex subexpression
	// used to capture the Value field value.
	perfDataValueFieldSubexpName string = "Value"

	// perfDataUoMFieldSubexpName is the name of the regex subexpression used
	// to capture the UoM field value.
	perfDataUoMFieldSubexpName string = "UoM"

	// perfDataValueAndUoMFieldsRegex is used to build capture groups for
	// "Value" and "UoM". The "Value" capture group is a required match
	// whereas the "UoM" capture group is optional.
	perfDataValueAndUoMFieldsRegex string = `(?P<Value>[-0-9.]+)(?P<UoM>[^\d;'"]*)?`

	// perfDataUnitOfMeasurementRegex represents the regex negated character
	// class used to validate the UnitOfMeasurement field.
	//
	// NOTE: UnitOfMeasurement field may be empty.
	// perfDataUnitOfMeasurementRegex string = `[^0-9;"']+`
)

// PerformanceData represents the performance data generated by a Nagios
// plugin.
//
// Plugin performance data is external data specific to the plugin used to
// perform the host or service check. Plugin-specific data can include things
// like percent packet loss, free disk space, processor load, number of
// current users, etc. - basically any type of metric that the plugin is
// measuring when it executes.
type PerformanceData struct {

	// Label is the text string used as a label for a specific performance
	// data point. The label length is arbitrary, but ideally the first 19
	// characters are unique due to a limitation in RRD. There is also a
	// limitation in the amount of data that NRPE returns to Nagios. When
	// emitted by a Nagios plugin, single quotes are required if spaces are in
	// the label.
	//
	// The popular convention used by plugin authors (and official
	// documentation) is to use underscores for separating multiple words. For
	// example, 'percent_packet_loss' instead of 'percent packet loss',
	// 'percentPacketLoss' or 'percent-packet-loss'.
	Label string

	// Value is the data point associated with the performance data label.
	//
	// Value is in class [-0-9.] and must be the same UOM as Min and Max UOM.
	// Value may be a literal "U" instead, this would indicate that the actual
	// value couldn't be determined.
	Value string

	// UnitOfMeasurement is an optional unit of measurement (UOM). If
	// provided, consists of a string of zero or more characters. Numbers,
	// semicolons or quotes are not permitted.
	//
	// Examples:
	//
	// 1) no unit specified - assume a number (int or float) of things (eg,
	// users, processes, load averages)
	// 2) s - seconds (also us, ms)
	// 3) % - percentage
	// 4) B - bytes (also KB, MB, TB)
	// 5) c - a continuous counter (such as bytes transmitted on an interface)
	//
	// NOTE: nagios-plugins.org uses "some examples" wording,
	// monitoring-plugins.org uses "one of" wording to refer to the examples,
	// implying that *only* the listed examples are supported. Icinga 2
	// documentation indicates that unknown UoMs are discarded (as if not
	// specified).
	UnitOfMeasurement string

	// Warn is in the range format (see the Section called Threshold and
	// Ranges). Must be the same UOM as Crit. An empty string is permitted.
	//
	// https://nagios-plugins.org/doc/guidelines.html#THRESHOLDFORMAT
	Warn string

	// Crit is in the range format (see the Section called Threshold and
	// Ranges). Must be the same UOM as Warn. An empty string is permitted.
	//
	// https://nagios-plugins.org/doc/guidelines.html#THRESHOLDFORMAT
	Crit string

	// Min is in class [-0-9.] and must be the same UOM as Value and Max. Min
	// is not required if UOM=%. An empty string is permitted.
	Min string

	// Max is in class [-0-9.] and must be the same UOM as Value and Min. Max
	// is not required if UOM=%. An empty string is permitted.
	Max string
}

// ParsePerfData parses a raw performance data string into a collection of
// PerformanceData values. The expected input format is:
//
//	'label'=value[UOM];[warn];[crit];[min];[max]
//
// Single quotes around the label are optional (if it does not contain
// spaces). Some fields are also optional. See the [Nagios Plugin Dev
// Guidelines] for additional details.
//
// [Nagios Plugin Dev Guidelines]: https://nagios-plugins.org/doc/guidelines.html#AEN200
func ParsePerfData(rawPerfdata string) ([]PerformanceData, error) {

	if strings.TrimSpace(rawPerfdata) == "" {
		return nil, fmt.Errorf(
			"missing input performance data string: %w",
			ErrInvalidPerformanceDataFormat,
		)
	}

	// Remove any double quotes if present.
	rawPerfdata = strings.Trim(rawPerfdata, `"`)

	// DEBUG
	// fmt.Printf("rawPerfdata without double quotes: %s\n", rawPerfdata)

	// Split raw perfdata string into individual metrics using whitespace
	// separators.
	//
	// This turns an input string such as:
	//
	// load1=0.260;5.000;10.000;0; load5=0.320;4.000;6.000;0; load15=0.300;3.000;4.000;0;
	//
	// into a collection of perfdata strings such as:
	//
	// load1=0.260;5.000;10.000;0;
	// load5=0.320;4.000;6.000;0;
	// load15=0.300;3.000;4.000;0;
	//
	//
	// If we are working with a single metric we get back that one metric, so
	// we're working from at least a slice of one element.
	perfdataStrings := strings.Fields(rawPerfdata)

	// DEBUG
	// fmt.Printf("space separated fields from rawPerfdata: %q\n", perfdataStrings)

	results := make([]PerformanceData, 0, len(perfdataStrings))

	for _, perfdataString := range perfdataStrings {
		perfdata, err := parsePerfData(perfdataString)
		if err != nil {
			return nil, err
		}
		results = append(results, perfdata)
	}

	return results, nil
}

// Validate performs basic validation of PerformanceData. An error is returned
// for any validation failures.
func (pd PerformanceData) Validate() error {

	// Validate fields
	switch {
	case pd.Label == "":
		return ErrPerformanceDataMissingLabel
	case pd.Value == "":
		return ErrPerformanceDataMissingValue

	// TODO: Expand validation
	// https://nagios-plugins.org/doc/guidelines.html
	default:
		return nil

	}
}

// String provides a PerformanceData metric in format ready for use in plugin
// output.
func (pd PerformanceData) String() string {
	return fmt.Sprintf(
		// The expected format of a performance data metric:
		//
		// 'label'=value[UOM];[warn];[crit];[min];[max]
		//
		// References:
		//
		// https://nagios-plugins.org/doc/guidelines.html
		// https://assets.nagios.com/downloads/nagioscore/docs/nagioscore/3/en/perfdata.html
		// https://assets.nagios.com/downloads/nagioscore/docs/nagioscore/3/en/pluginapi.html
		// https://www.monitoring-plugins.org/doc/guidelines.html
		// https://icinga.com/docs/icinga-2/latest/doc/05-service-monitoring/#performance-data-metrics
		" '%s'=%s%s;%s;%s;%s;%s",
		pd.Label,
		pd.Value,
		pd.UnitOfMeasurement,
		pd.Warn,
		pd.Crit,
		pd.Min,
		pd.Max,
	)
}

// parsePerfData parses an input string representing a performance data
// emitted by a Nagios plugin metric such as "load1=0.260;5.000;10.000;0;" (no
// quotes) into a PerformanceData value.
func parsePerfData(perfdataString string) (PerformanceData, error) {

	// Split based on semicolons.
	//
	// This turns an input string such as:
	//
	// load1=0.260;5.000;10.000;0;
	//
	// into:
	//
	// load1=0.260 (label of "load1", separator of "=", value of "0.260")
	// 5.000 (Warn; warning threshold)
	// 10.000 (Crit; critical threshold)
	// 0 (Min)
	//
	// It is possible that no semicolons are provided, in which case it is
	// assumed that we are working with a single performance data metric.
	perfdataFields := strings.Split(perfdataString, ";")

	// After splitting the input string on using a semicolon as separator
	// there must be a minimum of one field (i.e., no semicolons present) and
	// no more than the maximum/expected number based on the total fields of a
	// performance data metric string.
	switch numFields := len(perfdataFields); {
	case numFields < perfDataMinSemicolonSeparatedFields:
		return PerformanceData{}, fmt.Errorf(
			"input appears to be empty; after processing %d fields found; expected minimum of %d: %w",
			numFields,
			perfDataMinSemicolonSeparatedFields,
			ErrInvalidPerformanceDataFormat,
		)

	case numFields > perfDataMaxSemicolonSeparatedFields:
		return PerformanceData{}, fmt.Errorf(
			"input contains %d semicolon separated fields; expected no more than %d: %w",
			numFields,
			perfDataMaxSemicolonSeparatedFields,
			ErrInvalidPerformanceDataFormat,
		)
	}

	// DEBUG
	// fmt.Printf(
	// 	"%d semicolon separated fields from perfdataString: %q\n",
	// 	len(perfdataFields),
	// 	perfdataFields,
	// )

	label, rawValue, err := extractLabelAndRawValue(perfdataFields[0])
	if err != nil {
		return PerformanceData{}, fmt.Errorf("failed to extract label and raw value: %w", err)
	}

	value, uom, err := extractValueAndUoM(rawValue)
	if err != nil {
		return PerformanceData{}, fmt.Errorf("failed to extract value and uom: %w", err)
	}

	rawWarn, rawCrit, rawMin, rawMax := extractRawWarnCritMinMaxRawFieldVals(perfdataFields)

	warn, err := parsePerfDataWarnField(rawWarn)
	if err != nil {
		return PerformanceData{}, fmt.Errorf("failed to parse warn field: %w", err)
	}

	crit, err := parsePerfDataCritField(rawCrit)
	if err != nil {
		return PerformanceData{}, fmt.Errorf("failed to parse crit field: %w", err)
	}

	min, err := parsePerfDataMinField(rawMin)
	if err != nil {
		return PerformanceData{}, fmt.Errorf("failed to parse min field: %w", err)
	}

	max, err := parsePerfDataMaxField(rawMax)
	if err != nil {
		return PerformanceData{}, fmt.Errorf("failed to parse max field: %w", err)
	}

	perfdata := PerformanceData{
		Label:             label,
		Value:             value,
		UnitOfMeasurement: uom,
		Warn:              warn,
		Crit:              crit,
		Min:               min,
		Max:               max,
	}

	return perfdata, nil

}

// extractLabelAndRawValue processes a given input string and extracts a Label
// and a "raw" Value. The extracted "raw" Value requires further processing by
// another helper function to extract the Value and Unit of Measurement. An
// error is returned if parsing/validation fails.
//
// NOTE:
//
// The input string should NOT contain semicolons. The exported function
// (which calls this helper function) is responsible for splitting the "raw"
// performance data string first on spaces (individual performance data
// metric), then on semicolons (fields in a performance data metric).
func extractLabelAndRawValue(input string) (string, string, error) {

	if input == "" {
		return "", "", fmt.Errorf(
			"func extractLabelAndRawValue: empty input provided: %w",
			ErrInvalidPerformanceDataFormat,
		)
	}

	// Split on equals sign to obtain the label and the "raw" value. The raw
	// value contains the value and the (optional) Unit of Measurement (UoM).
	labelAndRawValue := strings.SplitN(input, "=", 2)

	if len(labelAndRawValue) != 2 {
		return "", "", fmt.Errorf(
			"failed to obtain metric label and raw value from field %q: %w",
			input,
			ErrInvalidPerformanceDataFormat,
		)
	}

	// Label field may have single quotes if the value contains spaces.
	label := strings.Trim(labelAndRawValue[0], `'`)

	// Label may have leading/trailing spaces (though unlikely).
	label = strings.TrimSpace(label)

	if err := validatePerfDataLabelField(label); err != nil {
		return "", "", fmt.Errorf(
			"failed to extract Label field from input string %q: %w",
			input,
			err,
		)
	}

	rawValue := strings.TrimSpace(labelAndRawValue[1])

	// We require a Value, so we go ahead and assert that we have something
	// before attempting any further processing.
	if rawValue == "" {
		return "", "", fmt.Errorf(
			"metric value is not present in input string %q: %w",
			input,
			ErrInvalidPerformanceDataFormat,
		)
	}

	return label, rawValue, nil
}

// extractValueAndUoM processes a given input string and extracts a Value and
// Unit of Measurement. An error is returned if parsing/validation fails.
//
// NOTE:
//
// The input string should NOT contain semicolons. The exported function
// (which calls this helper function) is responsible for splitting the "raw"
// performance data string first on spaces (individual performance data
// metric), then on semicolons (fields in a performance data metric).
func extractValueAndUoM(input string) (string, string, error) {

	if input == "" {
		return "", "", fmt.Errorf(
			"func extractValueAndUoM: empty input provided: %w",
			ErrInvalidPerformanceDataFormat,
		)
	}

	// Value may be a literal "U" (without quotes). If this is the case, there
	// will not be a Unit of Measurement and we can skip further input
	// parsing.
	if input == "U" {
		return input, "", nil
	}

	re := regexp.MustCompile(perfDataValueAndUoMFieldsRegex)

	matches := re.FindStringSubmatch(input)
	if len(matches) == 0 {
		return "", "", fmt.Errorf(
			"failed to extract Value and UoM fields from input string %q: %w",
			input,
			ErrInvalidPerformanceDataFormat,
		)
	}

	valIndex := re.SubexpIndex(perfDataValueFieldSubexpName)
	if valIndex < 0 {
		return "", "", fmt.Errorf(
			"failed to extract Value field from input string %q: %w",
			input,
			ErrInvalidPerformanceDataFormat,
		)
	}

	value := matches[valIndex]
	if err := validatePerfDataValueField(value); err != nil {
		return "", "", fmt.Errorf(
			"failed to extract Value field from input string %q: %w",
			input,
			err,
		)
	}

	var uom string
	uomIndex := re.SubexpIndex(perfDataUoMFieldSubexpName)
	if uomIndex >= 0 {
		uom = matches[uomIndex]
		if err := validatePerfDataUoMField(uom); err != nil {
			return "", "", fmt.Errorf(
				"failed to extract UnitOfMeasurement field from input string %q: %w",
				input,
				err,
			)
		}
		// DEBUG
		// fmt.Println(uom)
	}

	return value, uom, nil
}

// extractRawWarnCritMinMaxRawFieldVals processes a given collection of field
// values (obtained by splitting a performance data input string into separate
// fields) into Warn, Crit, Min and Max values. If values are not present for
// those fields an empty string is returned in its place.
func extractRawWarnCritMinMaxRawFieldVals(perfdataFields []string) (string, string, string, string) {
	var warn string
	if len(perfdataFields) >= 2 {
		warn = perfdataFields[1]
	}

	var crit string
	if len(perfdataFields) >= 3 {
		crit = perfdataFields[2]
	}

	var min string
	if len(perfdataFields) >= 4 {
		min = perfdataFields[3]
	}

	var max string
	if len(perfdataFields) >= 5 {
		max = perfdataFields[4]
	}

	return warn, crit, min, max
}

// parsePerfDataWarnField evaluates the given input string as a Performance
// Data "Warn" field value. An error is returned if validation fails,
// otherwise a sanitized version of the input string is returned.
func parsePerfDataWarnField(input string) (string, error) {
	input = strings.TrimSpace(input)

	err := validatePerfDataWarnField(input)
	if err != nil {
		return "", err
	}

	return input, nil
}

// parsePerfDataCritField evaluates the given input string as a Performance
// Data "Crit" field value. An error is returned if validation fails,
// otherwise a sanitized version of the input string is returned.
func parsePerfDataCritField(input string) (string, error) {
	input = strings.TrimSpace(input)

	err := validatePerfDataCritField(input)
	if err != nil {
		return "", err
	}

	return input, nil
}

// parsePerfDataMinField evaluates the given input string as a Performance
// Data "Min" field value. An error is returned if validation fails, otherwise
// a sanitized version of the input string is returned.
func parsePerfDataMinField(input string) (string, error) {
	input = strings.TrimSpace(input)

	err := validatePerfDataMinField(input)
	if err != nil {
		return "", err
	}

	return input, nil
}

// parsePerfDataMaxField evaluates the given input string as a Performance
// Data "Max" field value. An error is returned if validation fails, otherwise
// a sanitized version of the input string is returned.
func parsePerfDataMaxField(input string) (string, error) {
	input = strings.TrimSpace(input)

	err := validatePerfDataMaxField(input)
	if err != nil {
		return "", err
	}

	return input, nil
}

// validatePerfDataLabelField asserts that a given input string from the Label
// field of a parsed Performance Data value is in the correct format. An error
// is returned if validation fails.
//
// Validation is successful if:
//   - one or more characters
//   - any characters except the equals sign or single quote (')
//
// NOTE:
//
// While the Label field value should be single quoted if spaces are present
// in the string we do not require this as pre-processing removes all single
// quotes. Instead, validation fails if single quotes are found as this
// indicates that preprocessing was not performed prior to calling this
// validation function.
func validatePerfDataLabelField(input string) error {
	input = strings.TrimSpace(input)

	if input != "" &&
		!strings.ContainsAny(input, perfDataLabelFieldDisallowedCharacters) {
		return nil
	}

	invalidCharErr := fmt.Errorf(
		"input string %q contains disallowed character from set %q: %w",
		input,
		perfDataLabelFieldDisallowedCharacters,
		ErrInvalidPerformanceDataFormat,
	)

	// Assume the worst
	return fmt.Errorf(
		"field Label fails validation: %w",
		invalidCharErr,
	)
}

// validatePerfDataValueField asserts that a given input string from the Value
// field of a parsed Performance Data value is in the correct format. An error
// is returned if validation fails.
//
// Validation is successful if either is true:
//   - literal "U" character
//   - character class "[-0-9.]"
func validatePerfDataValueField(input string) error {
	input = strings.TrimSpace(input)

	re := regexp.MustCompile(perfDataValueFieldRegex)
	if re.MatchString(input) {
		return nil
	}

	// Assume the worst
	return fmt.Errorf(
		"field Value fails validation: %w",
		ErrInvalidPerformanceDataFormat,
	)
}

// validatePerfDataUoMField asserts that a given input string from the
// UnitOfMeasurement field of a parsed Performance Data value is in the
// correct format. An error is returned if validation fails.
//
// Validation is successful if:
//   - zero or more characters
//   - characters do not include numbers, semicolons, quotes
func validatePerfDataUoMField(input string) error {
	input = strings.TrimSpace(input)

	if input == "" {
		return nil
	}

	if !strings.ContainsAny(input, perfDataUoMFieldDisallowedCharacters) {
		return nil
	}

	invalidCharErr := fmt.Errorf(
		"input string %q contains disallowed character from set %q: %w",
		input,
		perfDataUoMFieldDisallowedCharacters,
		ErrInvalidPerformanceDataFormat,
	)

	// Assume the worst
	return fmt.Errorf(
		"field UnitOfMeasurement fails validation: %w",
		invalidCharErr,
	)
}

// validatePerfDataWarnField asserts that a given input string from the Warn
// field of a parsed Performance Data value is in the correct format. An error
// is returned if validation fails.
//
// Validation is successful if either is true:
//   - an empty string is permitted
//   - range format
func validatePerfDataWarnField(input string) error {

	input = strings.TrimSpace(input)

	if input == "" {
		return nil
	}

	re := regexp.MustCompile(perfDataThresholdRangeSyntaxRegex)
	if re.MatchString(input) {
		return nil
	}

	// Assume the worst
	return fmt.Errorf(
		"field Warn fails validation: %w",
		ErrInvalidPerformanceDataFormat,
	)
}

// validatePerfDataCritField asserts that a given input string from the Crit
// field of a parsed Performance Data value is in the correct format. An error
// is returned if validation fails.
//
// Validation is successful if either is true:
//   - an empty string is permitted
//   - range format
func validatePerfDataCritField(input string) error {

	input = strings.TrimSpace(input)

	if input == "" {
		return nil
	}

	re := regexp.MustCompile(perfDataThresholdRangeSyntaxRegex)
	if re.MatchString(input) {
		return nil
	}

	// Assume the worst
	return fmt.Errorf(
		"field Crit fails validation: %w",
		ErrInvalidPerformanceDataFormat,
	)
}

// validatePerfDataMinField asserts that a given input string from the Min
// field of a parsed Performance Data value is in the correct format. An error
// is returned if validation fails.
//
// Validation is successful if either is true:
//   - an empty string is permitted
//   - range format
func validatePerfDataMinField(input string) error {

	input = strings.TrimSpace(input)

	if input == "" {
		return nil
	}

	re := regexp.MustCompile(perfDataMinMaxFieldsRegex)
	if re.MatchString(input) {
		return nil
	}

	// Assume the worst
	return fmt.Errorf(
		"field Min fails validation: %w",
		ErrInvalidPerformanceDataFormat,
	)
}

// validatePerfDataMaxField asserts that a given input string from the Max
// field of a parsed Performance Data value is in the correct format. An error
// is returned if validation fails.
//
// Validation is successful if either is true:
//   - an empty string is permitted
//   - range format
func validatePerfDataMaxField(input string) error {

	input = strings.TrimSpace(input)

	if input == "" {
		return nil
	}

	re := regexp.MustCompile(perfDataMinMaxFieldsRegex)
	if re.MatchString(input) {
		return nil
	}

	// Assume the worst
	return fmt.Errorf(
		"field Max fails validation: %w",
		ErrInvalidPerformanceDataFormat,
	)
}
