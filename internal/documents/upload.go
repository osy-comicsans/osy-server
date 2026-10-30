package documents

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"unicode/utf8"
)

const (
	MaxFileBytes        = 10 << 20
	maxFields           = 30
	maxFieldValueLength = 200
)

// Types lists the documents a rider must provide, in the order the app shows them.
var Types = []string{"driver_license", "vehicle_registration"}

var fieldKeyPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]{0,39}$`)

// Upload is one submitted document. Fields holds the rider's confirmed OCR
// values as the JSON object the app sends.
type Upload struct {
	Type            string
	Source          string
	CaptureLanguage string
	Fields          string
	File            []byte
}

// Validate maps form field names to user-facing messages.
func (u Upload) Validate() map[string]string {
	errs := make(map[string]string)
	if !slices.Contains(Types, u.Type) {
		errs["type"] = "Must be driver_license or vehicle_registration"
	}
	if u.Source != "scan" && u.Source != "upload" {
		errs["source"] = "Must be scan or upload"
	}
	if u.CaptureLanguage != "fr" && u.CaptureLanguage != "ar" {
		errs["captureLanguage"] = "Must be fr or ar"
	}
	if message := fieldsError(u.Fields); message != "" {
		errs["fields"] = message
	}
	switch {
	case len(u.File) == 0:
		errs["file"] = "Attach the document"
	case len(u.File) > MaxFileBytes:
		errs["file"] = "File must be 10 MB or smaller"
	case u.MimeType() == "":
		errs["file"] = "Use a JPG, PNG or PDF file"
	}
	return errs
}

// MimeType reports the type found in the file's content, because the type the
// client declares cannot be trusted.
func (u Upload) MimeType() string {
	switch detected := http.DetectContentType(u.File); detected {
	case "image/jpeg", "image/png", "application/pdf":
		return detected
	}
	return ""
}

func fieldsError(raw string) string {
	var fields map[string]string
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return "Must be a JSON object of text values"
	}
	if len(fields) > maxFields {
		return fmt.Sprintf("Must have %d entries or fewer", maxFields)
	}
	for key, value := range fields {
		if !fieldKeyPattern.MatchString(key) {
			return "Keys must be camelCase names of 40 characters or fewer"
		}
		if utf8.RuneCountInString(value) > maxFieldValueLength {
			return fmt.Sprintf("Values must be %d characters or fewer", maxFieldValueLength)
		}
	}
	return ""
}
