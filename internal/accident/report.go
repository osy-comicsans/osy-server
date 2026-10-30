package accident

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Report struct {
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	DamagePercentage float64 `json:"damage_percentage"`
	RadiusMeters     int     `json:"radius_meters"`
}

type Target struct {
	Service    string
	ArabicName string
	Role       string
	Phone      string
}

type Alert struct {
	Service string
	Phone   string
	Message string
}

func Parse(message string, maxRadius int) (Report, error) {
	if maxRadius < 1 {
		maxRadius = 100000
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return Report{}, errors.New("message is empty")
	}
	parts := strings.FieldsFunc(message, func(r rune) bool {
		return r == '|' || r == ';' || r == '\n' || r == '\r'
	})
	if len(parts) == 0 || !strings.EqualFold(strings.TrimSpace(parts[0]), "OSY") {
		return Report{}, errors.New("message must start with OSY")
	}

	values := make(map[string]string, len(parts)-1)
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, found := strings.Cut(part, "=")
		if !found {
			return Report{}, fmt.Errorf("invalid field %q", part)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		values[key] = strings.TrimSpace(value)
	}

	latitude, err := requiredFloat(values, "latitude", "lat")
	if err != nil {
		return Report{}, err
	}
	longitude, err := requiredFloat(values, "longitude", "lon", "lng")
	if err != nil {
		return Report{}, err
	}
	damage, err := requiredFloat(values, "damage", "damage_percentage", "damage_percent")
	if err != nil {
		return Report{}, err
	}
	radiusValue, err := requiredValue(values, "radius", "range", "radius_meters")
	if err != nil {
		return Report{}, err
	}
	radius, err := parseRadius(radiusValue)
	if err != nil {
		return Report{}, err
	}

	if math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 {
		return Report{}, errors.New("latitude must be between -90 and 90")
	}
	if math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
		return Report{}, errors.New("longitude must be between -180 and 180")
	}
	if math.IsNaN(damage) || math.IsInf(damage, 0) || damage < 0 || damage > 100 {
		return Report{}, errors.New("damage percentage must be between 0 and 100")
	}
	if radius < 1 || radius > maxRadius {
		return Report{}, fmt.Errorf("radius must be between 1 and %d meters", maxRadius)
	}
	return Report{
		Latitude:         latitude,
		Longitude:        longitude,
		DamagePercentage: damage,
		RadiusMeters:     radius,
	}, nil
}

func DefaultTargets(civilProtection, gendarmerie, police string) []Target {
	return []Target{
		{
			Service:    "civil_protection",
			ArabicName: "الحماية المدنية",
			Role:       "الاستجابة للإصابات والحرائق والإنقاذ",
			Phone:      civilProtection,
		},
		{
			Service:    "national_gendarmerie",
			ArabicName: "الدرك الوطني",
			Role:       "التعامل مع الحوادث خارج النطاق الحضري وحوادث الطرق",
			Phone:      gendarmerie,
		},
		{
			Service:    "national_police",
			ArabicName: "الأمن الوطني",
			Role:       "التعامل مع الحوادث داخل المدن والمناطق الحضرية",
			Phone:      police,
		},
	}
}

func BuildAlerts(report Report, providerMessageID string, targets []Target) []Alert {
	alerts := make([]Alert, 0, len(targets))
	for _, target := range targets {
		if strings.TrimSpace(target.Phone) == "" {
			continue
		}
		alerts = append(alerts, Alert{
			Service: target.Service,
			Phone:   target.Phone,
			Message: arabicMessage(target, report, providerMessageID),
		})
	}
	return alerts
}

func arabicMessage(target Target, report Report, reference string) string {
	return fmt.Sprintf(`إلى %s:

نحن منصة OSYN، تلقّينا بلاغاً عن حادث.
مرجع البلاغ: %s
خط العرض: %.6f
خط الطول: %.6f
نسبة الأضرار التقديرية: %.1f%%
النطاق التقريبي: %s
الدور المطلوب: %s.

يرجى التحقق والتدخل العاجل.
هذه رسالة آلية من منصة OSYN.`,
		target.ArabicName,
		reference,
		report.Latitude,
		report.Longitude,
		report.DamagePercentage,
		formatRadius(report.RadiusMeters),
		target.Role,
	)
}

func formatRadius(meters int) string {
	if meters >= 1000 {
		return fmt.Sprintf("%.1f كيلومتر", float64(meters)/1000)
	}
	return fmt.Sprintf("%d متر", meters)
}

func requiredValue(values map[string]string, keys ...string) (string, error) {
	for _, key := range keys {
		if value, ok := values[key]; ok && value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("missing required field %s", keys[0])
}

func requiredFloat(values map[string]string, keys ...string) (float64, error) {
	value, err := requiredValue(values, keys...)
	if err != nil {
		return 0, err
	}
	value = strings.TrimSuffix(value, "%")
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q", keys[0], value)
	}
	return parsed, nil
}

func parseRadius(value string) (int, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	multiplier := 1.0
	switch {
	case strings.HasSuffix(normalized, "km"):
		multiplier = 1000
		normalized = strings.TrimSpace(strings.TrimSuffix(normalized, "km"))
	case strings.HasSuffix(normalized, "m"):
		normalized = strings.TrimSpace(strings.TrimSuffix(normalized, "m"))
	}
	parsed, err := strconv.ParseFloat(normalized, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("invalid radius value %q", value)
	}
	parsed *= multiplier
	if parsed > float64(math.MaxInt) {
		return 0, fmt.Errorf("radius value %q is too large", value)
	}
	return int(math.Round(parsed)), nil
}
