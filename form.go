package dave

import "net/url"

// ValidationErrors holds validation errors per field
type ValidationErrors map[string][]string

// Form holds form submission state and validation errors
type Form struct {
	State            url.Values
	ValidationErrors ValidationErrors
}

// NewFormResponse creates FormResponse with fields initialized
func NewForm() *Form {
	return &Form{
		State:            make(url.Values),
		ValidationErrors: make(ValidationErrors),
	}
}

// HasErrors returns true if there are any validation errors
func (f *Form) HasErrors() bool {
	return f != nil && len(f.ValidationErrors) > 0
}

// HasError returns true if the field has a validation error
func (f *Form) HasError(field string) bool {
	if f == nil || f.ValidationErrors == nil {
		return false
	}
	errors, exists := f.ValidationErrors[field]
	return exists && len(errors) > 0
}

// Errors returns the validation errors for a field, or nil
func (f *Form) Errors(field string) []string {
	if f == nil || f.ValidationErrors == nil {
		return nil
	}
	if errors, exists := f.ValidationErrors[field]; exists && len(errors) > 0 {
		return errors
	}
	return nil
}

// AddError adds a validation error for a field
func (f *Form) AddError(field, message string) {
	if f.ValidationErrors == nil {
		f.ValidationErrors = make(map[string][]string)
	}
	f.ValidationErrors[field] = append(f.ValidationErrors[field], message)
}

// Value returns the first value for a field, or the default if not set
func (f *Form) Value(field, defaultVal string) string {
	if f == nil || f.State == nil {
		return defaultVal
	}
	if vals, exists := f.State[field]; exists && len(vals) > 0 && vals[0] != "" {
		return vals[0]
	}
	return defaultVal
}

// Values returns all values for a field, or nil if not set
func (f *Form) Values(field string) []string {
	if f == nil || f.State == nil {
		return nil
	}
	if vals, exists := f.State[field]; exists {
		return vals
	}
	return nil
}
