package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/skamensky/google-automation/internal/googlecontacts"
	"github.com/spf13/cobra"
)

func newContactsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "contacts",
		Aliases: []string{"contact"},
		Short:   "List and inspect Google contacts",
	}

	cmd.AddCommand(newContactsListCommand(cfg))
	cmd.AddCommand(newContactsGetCommand(cfg))
	cmd.AddCommand(newContactsSearchCommand(cfg))
	cmd.AddCommand(newContactsUpdateCommand(cfg))
	cmd.AddCommand(newContactsExportCommand(cfg))

	return cmd
}

func newContactsExportCommand(cfg *appConfig) *cobra.Command {
	var labels []string
	var family bool
	var hasEmail bool
	var personFields string
	var output string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export Google contacts as JSON",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			exportLabels := append([]string{}, labels...)
			ignoreMissingLabels := false
			if family {
				exportLabels = append(exportLabels, "Personal Family", "Family 1st Gen", "Family - In Law", "family")
				ignoreMissingLabels = true
			}

			client, err := googlecontacts.NewClient(ctx, cfg.contactsConfig())
			if err != nil {
				return err
			}

			contacts, err := client.Export(ctx, googlecontacts.ExportOptions{
				Labels:              exportLabels,
				IgnoreMissingLabels: ignoreMissingLabels,
				HasEmail:            hasEmail,
				PersonFields:        personFields,
			})
			if err != nil {
				return err
			}

			if output == "" {
				return googlecontacts.WritePeople(cmd.OutOrStdout(), true, contacts)
			}

			outputPath, err := filepath.Abs(output)
			if err != nil {
				return fmt.Errorf("resolve output path: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
				return fmt.Errorf("create output directory: %w", err)
			}
			file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return fmt.Errorf("open output file: %w", err)
			}
			defer file.Close()

			if err := googlecontacts.WritePeople(file, true, contacts); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), outputPath)
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&labels, "label", nil, "contact group label to include; repeat for multiple labels")
	cmd.Flags().BoolVar(&family, "family", false, "include known family contact labels")
	cmd.Flags().BoolVar(&hasEmail, "has-email", false, "only include contacts with at least one email address")
	cmd.Flags().StringVar(&personFields, "person-fields", googlecontacts.DefaultPersonFields, "comma-separated People API fields to export")
	cmd.Flags().StringVar(&output, "output", "", "write JSON to this file instead of stdout")

	return cmd
}

func newContactsListCommand(cfg *appConfig) *cobra.Command {
	var pageSize int64
	var personFields string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List contacts from Google Contacts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			client, err := googlecontacts.NewClient(ctx, cfg.contactsConfig())
			if err != nil {
				return err
			}

			contacts, err := client.List(ctx, googlecontacts.ListOptions{
				PageSize:     pageSize,
				PersonFields: personFields,
			})
			if err != nil {
				return err
			}

			return googlecontacts.WritePeople(cmd.OutOrStdout(), cfg.jsonOutput, contacts)
		},
	}

	cmd.Flags().Int64Var(&pageSize, "page-size", 100, "maximum contacts to return")
	cmd.Flags().StringVar(&personFields, "person-fields", googlecontacts.DefaultPersonFields, "comma-separated People API fields to request")

	return cmd
}

func newContactsSearchCommand(cfg *appConfig) *cobra.Command {
	var searchFields string
	var personFields string

	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Search all Google contacts locally",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			client, err := googlecontacts.NewClient(ctx, cfg.contactsConfig())
			if err != nil {
				return err
			}

			contacts, err := client.Search(ctx, googlecontacts.SearchOptions{
				Query:        args[0],
				SearchFields: searchFields,
				PersonFields: personFields,
			})
			if err != nil {
				return err
			}

			return googlecontacts.WritePeople(cmd.OutOrStdout(), cfg.jsonOutput, contacts)
		},
	}

	cmd.Flags().StringVar(&searchFields, "fields", googlecontacts.DefaultSearchFields, "comma-separated contact fields to search, or all")
	cmd.Flags().StringVar(&personFields, "person-fields", googlecontacts.DefaultPersonFields, "comma-separated People API fields to request and return")

	return cmd
}

func newContactsGetCommand(cfg *appConfig) *cobra.Command {
	var personFields string

	cmd := &cobra.Command{
		Use:   "get RESOURCE_NAME",
		Short: "Get one contact by resource name, for example people/c123",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			client, err := googlecontacts.NewClient(ctx, cfg.contactsConfig())
			if err != nil {
				return err
			}

			person, err := client.Get(ctx, googlecontacts.GetOptions{
				ResourceName: args[0],
				PersonFields: personFields,
			})
			if err != nil {
				return err
			}

			return googlecontacts.WritePerson(cmd.OutOrStdout(), cfg.jsonOutput, person)
		},
	}

	cmd.Flags().StringVar(&personFields, "person-fields", googlecontacts.DefaultPersonFields, "comma-separated People API fields to request")

	return cmd
}

func newContactsUpdateCommand(cfg *appConfig) *cobra.Command {
	var rawFields []string

	cmd := &cobra.Command{
		Use:   "update RESOURCE_NAME --field FIELD=VALUE",
		Short: "Update fields on a Google contact",
		Long:  "Update fields on a Google contact. Repeat --field for multiple values. Supported fields: emailAddresses, phoneNumbers. Values can optionally include a type prefix, for example emailAddresses=home:person@example.com.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			client, err := googlecontacts.NewClient(ctx, cfg.contactsConfig())
			if err != nil {
				return err
			}

			fields := make([]googlecontacts.FieldUpdate, 0, len(rawFields))
			for _, raw := range rawFields {
				field, err := googlecontacts.ParseFieldUpdate(raw)
				if err != nil {
					return err
				}
				fields = append(fields, field)
			}

			person, err := client.Update(ctx, googlecontacts.UpdateOptions{
				ResourceName: args[0],
				Fields:       fields,
			})
			if err != nil {
				return err
			}

			return googlecontacts.WritePerson(cmd.OutOrStdout(), cfg.jsonOutput, person)
		},
	}

	cmd.Flags().StringArrayVar(&rawFields, "field", nil, "field update as name=value; repeat for multiple updates")
	_ = cmd.MarkFlagRequired("field")

	return cmd
}
