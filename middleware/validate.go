package middleware

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"maybe-finance-backend/utils"
)

// Validate checks struct fields tagged with `validate:"..."`.
// Supported tags: required, min=N, max=N, email, oneof=a|b|c
// Returns a list of validation error messages (empty if valid).
func Validate(v interface{}) []string {
	var errs []string
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return errs
	}
	t := val.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fVal := val.Field(i)
		tag := field.Tag.Get("validate")
		if tag == "" {
			continue
		}

		rules := strings.Split(tag, ",")
		for _, rule := range rules {
			rule = strings.TrimSpace(rule)

			switch {
			case rule == "required":
				if isZero(fVal) {
					errs = append(errs, fmt.Sprintf("%s is required", jsonName(field)))
				}

			case strings.HasPrefix(rule, "min="):
				n, _ := strconv.ParseFloat(strings.TrimPrefix(rule, "min="), 64)
				if fVal.Kind() == reflect.Float64 && fVal.Float() < n {
					errs = append(errs, fmt.Sprintf("%s must be at least %v", jsonName(field), n))
				}
				if fVal.Kind() == reflect.Int && float64(fVal.Int()) < n {
					errs = append(errs, fmt.Sprintf("%s must be at least %v", jsonName(field), n))
				}

			case strings.HasPrefix(rule, "max="):
				n, _ := strconv.ParseFloat(strings.TrimPrefix(rule, "max="), 64)
				if fVal.Kind() == reflect.Float64 && fVal.Float() > n {
					errs = append(errs, fmt.Sprintf("%s must be at most %v", jsonName(field), n))
				}
				if fVal.Kind() == reflect.Int && float64(fVal.Int()) > n {
					errs = append(errs, fmt.Sprintf("%s must be at most %v", jsonName(field), n))
				}

			case rule == "email":
				if fVal.Kind() == reflect.String {
					s := fVal.String()
					if s != "" && (!strings.Contains(s, "@") || !strings.Contains(s, ".")) {
						errs = append(errs, fmt.Sprintf("%s must be a valid email", jsonName(field)))
					}
				}

			case strings.HasPrefix(rule, "oneof="):
				allowed := strings.Split(strings.TrimPrefix(rule, "oneof="), "|")
				s := fmt.Sprintf("%v", fVal.Interface())
				found := false
				for _, a := range allowed {
					if s == a {
						found = true
						break
					}
				}
				if !found {
					errs = append(errs, fmt.Sprintf("%s must be one of: %s", jsonName(field), strings.Join(allowed, ", ")))
				}
			}
		}
	}
	return errs
}

// ValidateAndRespond validates a struct and writes a 400 error response if invalid.
// Returns true if validation passed, false if it wrote an error response.
func ValidateAndRespond(w http.ResponseWriter, v interface{}) bool {
	errs := Validate(v)
	if len(errs) > 0 {
		utils.ErrorResponse(w, http.StatusBadRequest, strings.Join(errs, "; "))
		return false
	}
	return true
}

func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Float64, reflect.Float32:
		return v.Float() == 0
	case reflect.Int, reflect.Int64:
		return v.Int() == 0
	case reflect.Bool:
		return false // bool zero is valid
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	default:
		return v.IsZero()
	}
}

func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	parts := strings.Split(tag, ",")
	return parts[0]
}
