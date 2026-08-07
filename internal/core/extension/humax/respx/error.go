package respx

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/validation"
)

// errorEnvelope keeps the public {code,message,meta,data} shape and retains
// structured validation details internally until request-locale serialization.
type errorEnvelope struct {
	status  int
	summary string
	details []errorDetail

	Code    int    `json:"code" doc:"the HTTP status code (1..999); business codes are 100000+"`
	Message string `json:"message" doc:"human-readable error summary"`
	Meta    Meta   `json:"meta" doc:"request metadata"`
	Data    any    `json:"data" doc:"always null on error"`
}

type errorDetail struct {
	message  string
	location string
}

// msgSep separates the summary key from the field detail, and detailSep joins
// multiple field details, inside errorEnvelope.Message. localize splits on them
// to translate the summary and each detail token independently.
const (
	msgSep    = ": "
	detailSep = "; "
	argSep    = "\x1f"
)

func (e *errorEnvelope) Error() string  { return e.Message }
func (e *errorEnvelope) GetStatus() int { return e.status }

// Err returns a business error carried in the envelope: a business Code
// (100000+) and an i18n message key, with meta filled from ctx. Return it from a
// handler and huma renders it as the standard error envelope; LocalizeMessage
// resolves key to the request locale at serialize time (unknown keys pass
// through unchanged). The transport status is 400 so the response stays under the
// documented error schema; the specific failure is conveyed by Code.
func Err(ctx context.Context, code int, key string) error {
	return &errorEnvelope{
		status:  http.StatusBadRequest,
		summary: key,
		Code:    code,
		Message: key,
		Meta:    metaOf(ctx, nil),
	}
}

// phraseKey normalizes huma's built-in English error summaries to i18n keys so
// framework-generated errors (e.g. request validation) localize like the rest.
// App code should pass i18n keys directly to huma.ErrorXxx / respx.Err rather
// than rely on this map; only huma's own fixed phrases need normalizing here.
var phraseKey = map[string]string{
	"validation failed":         "validation_failed",
	"request body is required":  "request_body_required",
	"request body read timeout": "request_body_timeout",
	"cannot read request body":  "request_body_unreadable",
}

// Install replaces huma.NewError so every built-in error response (validation,
// 4xx, 5xx) is rendered as the uniform envelope. Call once at startup BEFORE any
// huma.Register: huma's defineErrors reflects NewError's return type, so calling
// Install first also makes the generated OpenAPI document the error envelope.
// Summary and structured validation details are localized during serialization;
// arbitrary internal errors are not exposed. RequestAt is emitted in UTC;
// elapsed_ms is zero because huma builds errors without the
// request context that Timing populates. Code is the HTTP status (1..999);
// domain code mapping (100000+) can be layered on here later.
func Install() {
	installValidationMessages()
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		summary := normalizeSummary(msg)
		return &errorEnvelope{
			status:  status,
			summary: summary,
			details: detailOf(errs),
			Code:    status,
			Message: summary,
			Meta:    Meta{RequestAt: time.Now().UTC()},
			Data:    nil,
		}
	}
}

func normalizeSummary(message string) string {
	if key, ok := phraseKey[message]; ok {
		return key
	}
	const prefix = "request body is too large limit="
	if strings.HasPrefix(message, prefix) {
		limit := strings.TrimSuffix(strings.TrimPrefix(message, prefix), " bytes")
		return "request_body_too_large" + argSep + limit
	}
	return message
}

// detailOf keeps only structured, client-safe validation details. Arbitrary
// internal errors are intentionally omitted from public responses.
func detailOf(errs []error) []errorDetail {
	details := make([]errorDetail, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		de, ok := err.(huma.ErrorDetailer)
		if !ok {
			continue
		}
		detail := de.ErrorDetail()
		if detail != nil && detail.Message != "" {
			details = append(details, errorDetail{message: detail.Message, location: detail.Location})
		}
	}
	return details
}

func validationMessage(key string, args ...string) string {
	return key + argSep + strings.Join(args, argSep)
}

// Huma exposes its validation messages as variables. Replacing them with
// stable keys keeps every framework-generated field error localizable without
// parsing English text after validation has already run.
func installValidationMessages() {
	validation.MsgUnexpectedProperty = "validation_unexpected_property"
	validation.MsgExpectedRFC3339DateTime = "validation_expected_rfc3339_datetime"
	validation.MsgExpectedRFC1123DateTime = "validation_expected_rfc1123_datetime"
	validation.MsgExpectedRFC3339Date = "validation_expected_rfc3339_date"
	validation.MsgExpectedRFC3339Time = "validation_expected_rfc3339_time"
	validation.MsgExpectedRFC5322Email = validationMessage("validation_expected_email", "%v")
	validation.MsgExpectedRFC5890Hostname = "validation_expected_hostname"
	validation.MsgExpectedRFC2673IPv4 = "validation_expected_ipv4"
	validation.MsgExpectedRFC2373IPv6 = "validation_expected_ipv6"
	validation.MsgExpectedRFCIPAddr = "validation_expected_ip_address"
	validation.MsgExpectedRFC3986URI = validationMessage("validation_expected_uri", "%v")
	validation.MsgExpectedRFC4122UUID = validationMessage("validation_expected_uuid", "%v")
	validation.MsgExpectedRFC6570URITemplate = "validation_expected_uri_template"
	validation.MsgExpectedRFC6901JSONPointer = "validation_expected_json_pointer"
	validation.MsgExpectedRFC6901RelativeJSONPointer = "validation_expected_relative_json_pointer"
	validation.MsgExpectedRegexp = validationMessage("validation_expected_regex", "%v")
	validation.MsgExpectedMatchAtLeastOneSchema = "validation_expected_schema_match"
	validation.MsgExpectedMatchExactlyOneSchema = "validation_expected_single_schema_match"
	validation.MsgExpectedNotMatchSchema = "validation_expected_schema_mismatch"
	validation.MsgExpectedPropertyNameInObject = "validation_expected_property_name"
	validation.MsgExpectedBoolean = "validation_expected_boolean"
	validation.MsgExpectedDuration = validationMessage("validation_expected_duration", "%v")
	validation.MsgExpectedNumber = "validation_expected_number"
	validation.MsgExpectedInteger = "validation_expected_integer"
	validation.MsgExpectedString = "validation_expected_string"
	validation.MsgExpectedBase64String = "validation_expected_base64"
	validation.MsgExpectedArray = "validation_expected_array"
	validation.MsgExpectedObject = "validation_expected_object"
	validation.MsgExpectedArrayItemsUnique = "validation_expected_unique_items"
	validation.MsgExpectedOneOf = validationMessage("validation_expected_one_of", "%s")
	validation.MsgExpectedMinimumNumber = validationMessage("validation_expected_minimum_number", "%v")
	validation.MsgExpectedExclusiveMinimumNumber = validationMessage("validation_expected_exclusive_minimum_number", "%v")
	validation.MsgExpectedMaximumNumber = validationMessage("validation_expected_maximum_number", "%v")
	validation.MsgExpectedExclusiveMaximumNumber = validationMessage("validation_expected_exclusive_maximum_number", "%v")
	validation.MsgExpectedNumberBeMultipleOf = validationMessage("validation_expected_multiple", "%v")
	validation.MsgExpectedMinLength = validationMessage("validation_expected_min_length", "%d")
	validation.MsgExpectedMaxLength = validationMessage("validation_expected_max_length", "%d")
	validation.MsgExpectedBePattern = validationMessage("validation_expected_pattern", "%s")
	validation.MsgExpectedMatchPattern = validationMessage("validation_expected_pattern_match", "%s")
	validation.MsgExpectedMinItems = validationMessage("validation_expected_min_items", "%d")
	validation.MsgExpectedMaxItems = validationMessage("validation_expected_max_items", "%d")
	validation.MsgExpectedMinProperties = validationMessage("validation_expected_min_properties", "%d")
	validation.MsgExpectedMaxProperties = validationMessage("validation_expected_max_properties", "%d")
	validation.MsgExpectedRequiredProperty = validationMessage("validation_required_property", "%s")
	validation.MsgExpectedDependentRequiredProperty = validationMessage("validation_dependent_property", "%s", "%s")
}
