package googlecontacts

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	people "google.golang.org/api/people/v1"
)

func WritePeople(w io.Writer, asJSON bool, peopleList []*people.Person) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(peopleList)
	}

	for _, person := range peopleList {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			person.ResourceName,
			firstValue(person.Names, func(v *people.Name) string { return v.DisplayName }),
			firstValue(person.EmailAddresses, func(v *people.EmailAddress) string { return v.Value }),
			firstValue(person.PhoneNumbers, func(v *people.PhoneNumber) string { return v.Value }),
		)
	}

	return nil
}

func WritePerson(w io.Writer, asJSON bool, person *people.Person) error {
	return WritePeople(w, asJSON, []*people.Person{person})
}

func firstValue[T any](items []T, get func(T) string) string {
	for _, item := range items {
		value := strings.TrimSpace(get(item))
		if value != "" {
			return value
		}
	}
	return "-"
}
