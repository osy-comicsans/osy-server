package auth

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var mobileNumberPattern = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

type RegisterRequest struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	Email        string `json:"email"`
	MobileNumber string `json:"mobileNumber"`
	Password     string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ValidationError maps request field names to user-facing messages.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return "invalid registration request"
}

func (r RegisterRequest) normalize() RegisterRequest {
	return RegisterRequest{
		FirstName:    strings.TrimSpace(r.FirstName),
		LastName:     strings.TrimSpace(r.LastName),
		Email:        normalizeEmail(r.Email),
		MobileNumber: strings.TrimSpace(r.MobileNumber),
		Password:     r.Password,
	}
}

func (r RegisterRequest) validate() map[string]string {
	fields := make(map[string]string)
	if message := nameError(r.FirstName); message != "" {
		fields["firstName"] = message
	}
	if message := nameError(r.LastName); message != "" {
		fields["lastName"] = message
	}
	if !validEmail(r.Email) {
		fields["email"] = "Enter a valid email address"
	}
	if !mobileNumberPattern.MatchString(r.MobileNumber) {
		fields["mobileNumber"] = "Enter a valid mobile number"
	}
	if message := passwordError(r.Password); message != "" {
		fields["password"] = message
	}
	return fields
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func nameError(name string) string {
	length := utf8.RuneCountInString(name)
	if length < 2 {
		return "Must be at least 2 characters"
	}
	if length > 50 {
		return "Must be 50 characters or fewer"
	}
	return ""
}

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return false
	}
	domain := email[strings.LastIndex(email, "@")+1:]
	return strings.Contains(domain, ".")
}

func passwordError(password string) string {
	switch {
	case utf8.RuneCountInString(password) < 8:
		return "Must be at least 8 characters"
	// bcrypt only hashes the first 72 bytes.
	case len(password) > 72:
		return "Must be 72 characters or fewer"
	case !strings.ContainsFunc(password, unicode.IsLower):
		return "Must include a lowercase letter"
	case !strings.ContainsFunc(password, unicode.IsUpper):
		return "Must include an uppercase letter"
	case !strings.ContainsFunc(password, unicode.IsDigit):
		return "Must include a number"
	}
	return ""
}
