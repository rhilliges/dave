package dave

import "net/url"

// ValidationErrors holds validation errors per field
type ValidationErrors map[string][]string

// FormResponse holds form submission state and validation errors
type FormResponse struct {
	State            url.Values
	ValidationErrors ValidationErrors
	Result           any
}

// NewFormResponse creates FormResponse with fields initialized
func NewFormResponse() *FormResponse {
	return &FormResponse{
		State:            make(url.Values),
		ValidationErrors: make(ValidationErrors),
	}
}

// HasErrors returns true if there are any validation errors
func (f *FormResponse) HasErrors() bool {
	return f != nil && len(f.ValidationErrors) > 0
}

// HasError returns true if the field has a validation error
func (f *FormResponse) HasError(field string) bool {
	if f == nil || f.ValidationErrors == nil {
		return false
	}
	errors, exists := f.ValidationErrors[field]
	return exists && len(errors) > 0
}

// Errors returns the validation errors for a field, or nil
func (f *FormResponse) Errors(field string) []string {
	if f == nil || f.ValidationErrors == nil {
		return nil
	}
	if errors, exists := f.ValidationErrors[field]; exists && len(errors) > 0 {
		return errors
	}
	return nil
}

// AddError adds a validation error for a field
func (f *FormResponse) AddError(field, message string) {
	if f.ValidationErrors == nil {
		f.ValidationErrors = make(map[string][]string)
	}
	f.ValidationErrors[field] = append(f.ValidationErrors[field], message)
}

// Value returns the first value for a field, or the default if not set
func (f *FormResponse) Value(field, defaultVal string) string {
	if f == nil || f.State == nil {
		return defaultVal
	}
	if vals, exists := f.State[field]; exists && len(vals) > 0 && vals[0] != "" {
		return vals[0]
	}
	return defaultVal
}

// Values returns all values for a field, or nil if not set
func (f *FormResponse) Values(field string) []string {
	if f == nil || f.State == nil {
		return nil
	}
	if vals, exists := f.State[field]; exists {
		return vals
	}
	return nil
}
