package googlecontacts

import (
	"context"
	"fmt"
	"net/mail"
	"sort"
	"strings"

	"github.com/skamensky/google-automation/internal/cache"
	"github.com/skamensky/google-automation/internal/googleauth"
	people "google.golang.org/api/people/v1"
)

const DefaultPersonFields = "names,emailAddresses,phoneNumbers,organizations,addresses,biographies,metadata"
const DefaultSearchFields = "names,emailAddresses,phoneNumbers,organizations"

type Config struct {
	CredentialsFile string
	TokenFile       string
	NoBrowser       bool
	Cache           cache.Config
}

type Client struct {
	service *people.Service
	cache   cache.Store
}

type ListOptions struct {
	PageSize     int64
	PersonFields string
}

type GetOptions struct {
	ResourceName string
	PersonFields string
}

type FieldUpdate struct {
	Field string
	Value string
	Type  string
}

type UpdateOptions struct {
	ResourceName string
	Fields       []FieldUpdate
}

type SearchOptions struct {
	Query        string
	SearchFields string
	PersonFields string
}

type ExportOptions struct {
	Labels              []string
	IgnoreMissingLabels bool
	HasEmail            bool
	PersonFields        string
}

func Scopes() []string {
	return []string{
		people.ContactsScope,
		people.UserinfoEmailScope,
		people.UserinfoProfileScope,
	}
}

func Login(ctx context.Context, cfg Config) error {
	_, err := googleauth.Login(ctx, googleauth.Config{
		CredentialsFile: cfg.CredentialsFile,
		TokenFile:       cfg.TokenFile,
		Scopes:          Scopes(),
		NoBrowser:       cfg.NoBrowser,
	})
	return err
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	httpClient, err := googleauth.NewClient(ctx, googleauth.Config{
		CredentialsFile: cfg.CredentialsFile,
		TokenFile:       cfg.TokenFile,
		Scopes:          Scopes(),
		NoBrowser:       cfg.NoBrowser,
	})
	if err != nil {
		return nil, err
	}

	service, err := people.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("create people service: %w", err)
	}

	return &Client{
		service: service,
		cache:   cache.NewStore(cfg.Cache),
	}, nil
}

func (c *Client) List(ctx context.Context, opts ListOptions) ([]*people.Person, error) {
	if opts.PageSize == 0 {
		opts.PageSize = 100
	}
	if opts.PersonFields == "" {
		opts.PersonFields = DefaultPersonFields
	}

	resp, err := c.service.People.Connections.List("people/me").
		PageSize(opts.PageSize).
		PersonFields(opts.PersonFields).
		SortOrder("FIRST_NAME_ASCENDING").
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}

	return resp.Connections, nil
}

func (c *Client) ListAll(ctx context.Context, opts ListOptions) ([]*people.Person, error) {
	if opts.PageSize == 0 {
		opts.PageSize = 1000
	}
	if opts.PersonFields == "" {
		opts.PersonFields = DefaultPersonFields
	}

	personFields, err := normalizedPersonFields(opts.PersonFields)
	if err != nil {
		return nil, err
	}

	var cached []*people.Person
	cacheKey := []string{"all", personFields}
	if ok, err := c.cache.GetJSON(ctx, "contacts", cacheKey, &cached); err != nil {
		return nil, err
	} else if ok {
		return cached, nil
	}

	var contacts []*people.Person
	var pageToken string
	for {
		call := c.service.People.Connections.List("people/me").
			PageSize(opts.PageSize).
			PersonFields(personFields).
			SortOrder("FIRST_NAME_ASCENDING").
			Context(ctx)
		if pageToken != "" {
			call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("list contacts: %w", err)
		}
		contacts = append(contacts, resp.Connections...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	if err := c.cache.SetJSON(ctx, "contacts", cacheKey, contacts); err != nil {
		return nil, err
	}

	return contacts, nil
}

func (c *Client) Get(ctx context.Context, opts GetOptions) (*people.Person, error) {
	if opts.PersonFields == "" {
		opts.PersonFields = DefaultPersonFields
	}

	person, err := c.service.People.Get(opts.ResourceName).
		PersonFields(opts.PersonFields).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("get contact %q: %w", opts.ResourceName, err)
	}

	return person, nil
}

func SupportedUpdateFields() []string {
	fields := make([]string, 0, len(updateFieldSet))
	for field := range updateFieldSet {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func (c *Client) Update(ctx context.Context, opts UpdateOptions) (*people.Person, error) {
	resourceName := strings.TrimSpace(opts.ResourceName)
	if resourceName == "" {
		return nil, fmt.Errorf("resource name is required")
	}
	if len(opts.Fields) == 0 {
		return nil, fmt.Errorf("at least one --field is required")
	}

	personFields, err := updatePersonFields(opts.Fields)
	if err != nil {
		return nil, err
	}
	latest, err := c.Get(ctx, GetOptions{
		ResourceName: resourceName,
		PersonFields: personFields + ",metadata",
	})
	if err != nil {
		return nil, err
	}

	contactSources := contactMetadataSources(latest)
	if len(contactSources) == 0 {
		return nil, fmt.Errorf("contact %q does not include contact-source metadata needed for update", resourceName)
	}

	update := &people.Person{
		ResourceName: latest.ResourceName,
		Etag:         latest.Etag,
		Metadata:     &people.PersonMetadata{Sources: contactSources},
	}

	for _, field := range opts.Fields {
		if err := applyFieldUpdate(update, latest, field); err != nil {
			return nil, err
		}
	}

	updated, err := c.service.People.UpdateContact(resourceName, update).
		UpdatePersonFields(personFields).
		PersonFields(DefaultPersonFields).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("update contact %q: %w", resourceName, err)
	}

	if err := c.cache.DeleteNamespace(ctx, "contacts"); err != nil {
		return nil, err
	}

	return updated, nil
}

func (c *Client) Search(ctx context.Context, opts SearchOptions) ([]*people.Person, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, fmt.Errorf("search query is required")
	}

	searchFields, err := normalizedSearchFields(opts.SearchFields)
	if err != nil {
		return nil, err
	}

	personFields, err := normalizedPersonFields(opts.PersonFields)
	if err != nil {
		return nil, err
	}
	personFields, err = unionPersonFields(personFields, strings.Join(searchFields, ","))
	if err != nil {
		return nil, err
	}

	contacts, err := c.ListAll(ctx, ListOptions{
		PageSize:     1000,
		PersonFields: personFields,
	})
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	matches := make([]*people.Person, 0)
	for _, contact := range contacts {
		if personMatches(contact, query, searchFields) {
			matches = append(matches, contact)
		}
	}

	return matches, nil
}

func (c *Client) Export(ctx context.Context, opts ExportOptions) ([]*people.Person, error) {
	personFields := opts.PersonFields
	if personFields == "" {
		personFields = DefaultPersonFields
	}
	personFields, err := unionPersonFields(personFields, "memberships,emailAddresses")
	if err != nil {
		return nil, err
	}

	labelResourceNames, err := c.labelResourceNames(ctx, opts.Labels, opts.IgnoreMissingLabels)
	if err != nil {
		return nil, err
	}

	contacts, err := c.ListAll(ctx, ListOptions{
		PageSize:     1000,
		PersonFields: personFields,
	})
	if err != nil {
		return nil, err
	}

	filtered := make([]*people.Person, 0, len(contacts))
	for _, contact := range contacts {
		if opts.HasEmail && len(contact.EmailAddresses) == 0 {
			continue
		}
		if len(labelResourceNames) > 0 && !personHasAnyLabel(contact, labelResourceNames) {
			continue
		}
		filtered = append(filtered, contact)
	}
	return filtered, nil
}

func (c *Client) labelResourceNames(ctx context.Context, labels []string, ignoreMissing bool) (map[string]struct{}, error) {
	if len(labels) == 0 {
		return nil, nil
	}

	wanted := make(map[string]struct{})
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		wanted[strings.ToLower(label)] = struct{}{}
	}
	if len(wanted) == 0 {
		return nil, nil
	}

	resources := make(map[string]struct{})
	foundLabels := make(map[string]struct{})
	var pageToken string
	for {
		call := c.service.ContactGroups.List().
			PageSize(1000).
			Context(ctx)
		if pageToken != "" {
			call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("list contact groups: %w", err)
		}
		for _, group := range resp.ContactGroups {
			if group == nil {
				continue
			}
			names := []string{group.ResourceName, group.Name, group.FormattedName}
			for _, name := range names {
				normalized := strings.ToLower(strings.TrimSpace(name))
				if _, ok := wanted[normalized]; ok {
					resources[group.ResourceName] = struct{}{}
					foundLabels[normalized] = struct{}{}
				}
			}
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	if len(resources) == 0 {
		return nil, fmt.Errorf("none of the requested labels were found: %s", strings.Join(sortedMapKeys(wanted), ","))
	}
	missing := make([]string, 0)
	for label := range wanted {
		if _, ok := foundLabels[label]; !ok {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 && !ignoreMissing {
		sort.Strings(missing)
		return nil, fmt.Errorf("requested labels not found: %s", strings.Join(missing, ","))
	}
	return resources, nil
}

func personHasAnyLabel(person *people.Person, labelResourceNames map[string]struct{}) bool {
	for _, membership := range person.Memberships {
		if membership == nil || membership.ContactGroupMembership == nil {
			continue
		}
		resourceName := strings.TrimSpace(membership.ContactGroupMembership.ContactGroupResourceName)
		if _, ok := labelResourceNames[resourceName]; ok {
			return true
		}
	}
	return false
}

func sortedMapKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func contactMetadataSources(person *people.Person) []*people.Source {
	if person == nil || person.Metadata == nil {
		return nil
	}

	sources := make([]*people.Source, 0, len(person.Metadata.Sources))
	for _, source := range person.Metadata.Sources {
		if source == nil || !strings.EqualFold(source.Type, "CONTACT") {
			continue
		}
		sources = append(sources, &people.Source{
			Etag: source.Etag,
			Id:   source.Id,
			Type: source.Type,
		})
	}
	return sources
}

var updateFieldSet = map[string]struct{}{
	"emailAddresses": {},
	"phoneNumbers":   {},
}

func updatePersonFields(fields []FieldUpdate) (string, error) {
	seen := make(map[string]struct{})
	for _, field := range fields {
		name := strings.TrimSpace(field.Field)
		if _, ok := updateFieldSet[name]; !ok {
			return "", fmt.Errorf("unsupported update field %q; supported fields: %s", name, strings.Join(SupportedUpdateFields(), ","))
		}
		seen[name] = struct{}{}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ","), nil
}

func ParseFieldUpdate(raw string) (FieldUpdate, error) {
	field, value, ok := strings.Cut(strings.TrimSpace(raw), "=")
	if !ok {
		return FieldUpdate{}, fmt.Errorf("field update %q must use name=value", raw)
	}
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" || value == "" {
		return FieldUpdate{}, fmt.Errorf("field update %q must include a non-empty name and value", raw)
	}

	valueType := ""
	if before, after, ok := strings.Cut(value, ":"); ok && before != "" && after != "" {
		valueType = strings.TrimSpace(before)
		value = strings.TrimSpace(after)
	}

	if _, ok := updateFieldSet[field]; !ok {
		return FieldUpdate{}, fmt.Errorf("unsupported update field %q; supported fields: %s", field, strings.Join(SupportedUpdateFields(), ","))
	}
	return FieldUpdate{Field: field, Value: value, Type: valueType}, nil
}

func applyFieldUpdate(update, latest *people.Person, field FieldUpdate) error {
	switch strings.TrimSpace(field.Field) {
	case "emailAddresses":
		email, err := normalizeEmail(field.Value)
		if err != nil {
			return err
		}
		if hasEmail(latest.EmailAddresses, email) {
			update.EmailAddresses = latest.EmailAddresses
			return nil
		}
		emailType := strings.TrimSpace(field.Type)
		if emailType == "" {
			emailType = "home"
		}
		update.EmailAddresses = append(append([]*people.EmailAddress{}, latest.EmailAddresses...), &people.EmailAddress{
			Type:  emailType,
			Value: email,
		})
	case "phoneNumbers":
		phone := strings.TrimSpace(field.Value)
		if phone == "" {
			return fmt.Errorf("phone number is required")
		}
		if hasPhone(latest.PhoneNumbers, phone) {
			update.PhoneNumbers = latest.PhoneNumbers
			return nil
		}
		phoneType := strings.TrimSpace(field.Type)
		if phoneType == "" {
			phoneType = "mobile"
		}
		update.PhoneNumbers = append(append([]*people.PhoneNumber{}, latest.PhoneNumbers...), &people.PhoneNumber{
			Type:  phoneType,
			Value: phone,
		})
	default:
		return fmt.Errorf("unsupported update field %q; supported fields: %s", field.Field, strings.Join(SupportedUpdateFields(), ","))
	}
	return nil
}

func normalizeEmail(value string) (string, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("invalid email address %q: %w", value, err)
	}
	email := strings.TrimSpace(strings.ToLower(address.Address))
	if email == "" {
		return "", fmt.Errorf("email address is required")
	}
	return email, nil
}

func hasEmail(items []*people.EmailAddress, email string) bool {
	for _, item := range items {
		if item != nil && strings.EqualFold(strings.TrimSpace(item.Value), email) {
			return true
		}
	}
	return false
}

func hasPhone(items []*people.PhoneNumber, phone string) bool {
	normalized := normalizePhone(phone)
	for _, item := range items {
		if item != nil && normalizePhone(item.Value) == normalized {
			return true
		}
	}
	return false
}

func normalizePhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
