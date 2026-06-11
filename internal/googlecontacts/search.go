package googlecontacts

import (
	"fmt"
	"sort"
	"strings"

	people "google.golang.org/api/people/v1"
)

var validPersonFields = map[string]struct{}{
	"addresses":      {},
	"ageRanges":      {},
	"biographies":    {},
	"birthdays":      {},
	"calendarUrls":   {},
	"clientData":     {},
	"coverPhotos":    {},
	"emailAddresses": {},
	"events":         {},
	"externalIds":    {},
	"genders":        {},
	"imClients":      {},
	"interests":      {},
	"locales":        {},
	"locations":      {},
	"memberships":    {},
	"metadata":       {},
	"miscKeywords":   {},
	"names":          {},
	"nicknames":      {},
	"occupations":    {},
	"organizations":  {},
	"phoneNumbers":   {},
	"photos":         {},
	"relations":      {},
	"sipAddresses":   {},
	"skills":         {},
	"urls":           {},
	"userDefined":    {},
}

var validSearchFields = map[string]struct{}{
	"addresses":      {},
	"biographies":    {},
	"emailAddresses": {},
	"names":          {},
	"nicknames":      {},
	"organizations":  {},
	"phoneNumbers":   {},
	"urls":           {},
	"userDefined":    {},
}

func normalizedPersonFields(value string) (string, error) {
	if value == "" {
		value = DefaultPersonFields
	}
	return normalizeFields(value, validPersonFields, "person")
}

func normalizedSearchFields(value string) ([]string, error) {
	if value == "" {
		value = DefaultSearchFields
	}
	if strings.EqualFold(strings.TrimSpace(value), "all") {
		return sortedKeys(validSearchFields), nil
	}
	normalized, err := normalizeFields(value, validSearchFields, "search")
	if err != nil {
		return nil, err
	}
	return strings.Split(normalized, ","), nil
}

func unionPersonFields(values ...string) (string, error) {
	set := make(map[string]struct{})
	for _, value := range values {
		normalized, err := normalizedPersonFields(value)
		if err != nil {
			return "", err
		}
		for _, field := range strings.Split(normalized, ",") {
			set[field] = struct{}{}
		}
	}

	fields := make([]string, 0, len(set))
	for field := range set {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return strings.Join(fields, ","), nil
}

func normalizeFields(value string, valid map[string]struct{}, kind string) (string, error) {
	seen := make(map[string]struct{})
	fields := make([]string, 0)
	for _, raw := range strings.Split(value, ",") {
		field := strings.TrimSpace(raw)
		if field == "" {
			continue
		}
		if strings.EqualFold(field, "all") {
			return "", fmt.Errorf("%s field \"all\" must be used by itself", kind)
		}
		if _, ok := valid[field]; !ok {
			validFields := sortedKeys(valid)
			if kind == "search" {
				validFields = append([]string{"all"}, validFields...)
			}
			return "", fmt.Errorf("invalid %s field %q; valid fields: %s", kind, field, strings.Join(validFields, ","))
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		fields = append(fields, field)
	}
	if len(fields) == 0 {
		return "", fmt.Errorf("at least one %s field is required", kind)
	}
	sort.Strings(fields)
	return strings.Join(fields, ","), nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func personMatches(person *people.Person, query string, fields []string) bool {
	for _, field := range fields {
		for _, value := range searchableValues(person, field) {
			if strings.Contains(strings.ToLower(value), query) {
				return true
			}
		}
	}
	return false
}

func searchableValues(person *people.Person, field string) []string {
	var values []string
	switch field {
	case "addresses":
		for _, item := range person.Addresses {
			values = append(values, item.FormattedValue, item.StreetAddress, item.City, item.Region, item.PostalCode, item.Country)
		}
	case "biographies":
		for _, item := range person.Biographies {
			values = append(values, item.Value)
		}
	case "emailAddresses":
		for _, item := range person.EmailAddresses {
			values = append(values, item.Value, item.DisplayName)
		}
	case "names":
		for _, item := range person.Names {
			values = append(values, item.DisplayName, item.DisplayNameLastFirst, item.FamilyName, item.GivenName, item.MiddleName, item.UnstructuredName)
		}
	case "nicknames":
		for _, item := range person.Nicknames {
			values = append(values, item.Value)
		}
	case "organizations":
		for _, item := range person.Organizations {
			values = append(values, item.Name, item.Title, item.Department, item.Domain, item.Location)
		}
	case "phoneNumbers":
		for _, item := range person.PhoneNumbers {
			values = append(values, item.Value, item.CanonicalForm)
		}
	case "urls":
		for _, item := range person.Urls {
			values = append(values, item.Value)
		}
	case "userDefined":
		for _, item := range person.UserDefined {
			values = append(values, item.Key, item.Value)
		}
	}
	return values
}
