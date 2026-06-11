package cli

import (
	"fmt"
	"strings"

	"github.com/skamensky/google-automation/internal/googledrive"
	"github.com/spf13/cobra"
)

func newDriveCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drive",
		Short: "Manage Google Drive files and folders",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(newDriveFilesCommand(cfg))
	cmd.AddCommand(newDriveFoldersCommand(cfg))
	return cmd
}

func newDriveFilesCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "files", Short: "Manage Drive files"}
	cmd.AddCommand(newDriveFilesSearchCommand(cfg))
	cmd.AddCommand(newDriveFilesGetCommand(cfg, false))
	cmd.AddCommand(newDriveFilesUpdateCommand(cfg, false))
	cmd.AddCommand(newDriveFilesMoveCommand(cfg, false))
	cmd.AddCommand(newDriveFilesTrashCommand(cfg, false))
	cmd.AddCommand(newDriveFilesDeleteCommand(cfg, false))
	cmd.AddCommand(newDriveFilesDownloadCommand(cfg))
	cmd.AddCommand(newDriveFilesUploadCommand(cfg))
	cmd.AddCommand(newDriveFilesDownloadZipCommand(cfg))
	cmd.AddCommand(newDrivePermissionsCommand(cfg))
	return cmd
}

func newDriveFilesSearchCommand(cfg *appConfig) *cobra.Command {
	var opts googledrive.SearchOptions
	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Search Drive files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				opts.Query = args[0]
			}
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			result, err := client.Search(commandContext(cmd), opts)
			if err != nil {
				return err
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), result)
		},
	}
	addDriveListFlags(cmd, &opts.PageSize, &opts.PageToken, &opts.Type, &opts.Trashed)
	cmd.Flags().StringVar(&opts.RawQuery, "q", "", "raw Drive API files.list query")
	return cmd
}

func newDriveFilesGetCommand(cfg *appConfig, folder bool) *cobra.Command {
	use := "get FILE_ID"
	short := "Get Drive file metadata"
	if folder {
		use = "get FOLDER_ID"
		short = "Get Drive folder metadata"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			file, err := client.Get(commandContext(cmd), args[0])
			if err != nil {
				return err
			}
			if folder && file.MimeType != googledrive.FolderMimeType {
				return fmt.Errorf("%q is not a folder", args[0])
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), file)
		},
	}
	return cmd
}

func newDriveFilesUpdateCommand(cfg *appConfig, folder bool) *cobra.Command {
	var rawFields []string
	use := "update FILE_ID --field name=..."
	short := "Update Drive file metadata"
	if folder {
		use = "update FOLDER_ID --field name=..."
		short = "Update Drive folder metadata"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fields, err := parseDriveUpdateFields(rawFields)
			if err != nil {
				return err
			}
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			if folder {
				file, err := client.Get(commandContext(cmd), args[0])
				if err != nil {
					return err
				}
				if file.MimeType != googledrive.FolderMimeType {
					return fmt.Errorf("%q is not a folder", args[0])
				}
			}
			file, err := client.Update(commandContext(cmd), googledrive.UpdateOptions{FileID: args[0], Fields: fields})
			if err != nil {
				return err
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), file)
		},
	}
	cmd.Flags().StringArrayVar(&rawFields, "field", nil, "metadata update as name=value; repeat for multiple updates")
	_ = cmd.MarkFlagRequired("field")
	return cmd
}

func newDriveFilesMoveCommand(cfg *appConfig, folder bool) *cobra.Command {
	var parent string
	use := "move FILE_ID --parent FOLDER_ID"
	short := "Move a Drive file"
	if folder {
		use = "move FOLDER_ID --parent DESTINATION_FOLDER_ID"
		short = "Move a Drive folder"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			file, err := client.Move(commandContext(cmd), googledrive.MoveOptions{FileID: args[0], Parent: parent, IsFolder: folder})
			if err != nil {
				return err
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), file)
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "destination folder ID")
	_ = cmd.MarkFlagRequired("parent")
	return cmd
}

func newDriveFilesTrashCommand(cfg *appConfig, folder bool) *cobra.Command {
	use := "trash FILE_ID"
	short := "Trash a Drive file"
	if folder {
		use = "trash FOLDER_ID"
		short = "Trash a Drive folder"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			file, err := client.Trash(commandContext(cmd), args[0], folder)
			if err != nil {
				return err
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), file)
		},
	}
}

func newDriveFilesDeleteCommand(cfg *appConfig, folder bool) *cobra.Command {
	var yes bool
	use := "delete FILE_ID --yes"
	short := "Permanently delete a Drive file"
	if folder {
		use = "delete FOLDER_ID --yes"
		short = "Permanently delete a Drive folder"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing to delete without --yes")
			}
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			if err := client.Delete(commandContext(cmd), args[0], folder); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm permanent deletion")
	return cmd
}

func newDriveFilesDownloadCommand(cfg *appConfig) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "download FILE_ID --output PATH",
		Short: "Download a binary Drive file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			if err := client.Download(commandContext(cmd), args[0], output); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), output)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "output file path")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newDriveFilesUploadCommand(cfg *appConfig) *cobra.Command {
	var name string
	var parent string
	cmd := &cobra.Command{
		Use:   "upload PATH",
		Short: "Upload a local file to Drive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			file, err := client.Upload(commandContext(cmd), googledrive.UploadOptions{
				Path:   args[0],
				Name:   name,
				Parent: parent,
			})
			if err != nil {
				return err
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), file)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Drive file name")
	cmd.Flags().StringVar(&parent, "parent", "root", "parent folder ID")
	return cmd
}

func newDriveFilesDownloadZipCommand(cfg *appConfig) *cobra.Command {
	var opts googledrive.DownloadZipOptions
	cmd := &cobra.Command{
		Use:   "download-zip --file FILE_ID --folder FOLDER_ID --output bundle.zip",
		Short: "Download selected Drive files and folders as a local zip",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			if err := client.DownloadZip(commandContext(cmd), opts); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), opts.Output)
			return nil
		},
	}
	addDownloadZipFlags(cmd, &opts)
	return cmd
}

func newDriveFoldersCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "folders", Short: "Manage Drive folders"}
	cmd.AddCommand(newDriveFoldersListCommand(cfg))
	cmd.AddCommand(newDriveFoldersCreateCommand(cfg))
	cmd.AddCommand(newDriveFilesGetCommand(cfg, true))
	cmd.AddCommand(newDriveFilesUpdateCommand(cfg, true))
	cmd.AddCommand(newDriveFilesMoveCommand(cfg, true))
	cmd.AddCommand(newDriveFoldersTreeCommand(cfg))
	cmd.AddCommand(newDriveFoldersDownloadCommand(cfg))
	cmd.AddCommand(newDriveFilesTrashCommand(cfg, true))
	cmd.AddCommand(newDriveFilesDeleteCommand(cfg, true))
	return cmd
}

func newDriveFoldersListCommand(cfg *appConfig) *cobra.Command {
	var opts googledrive.ListFolderOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List children of a Drive folder",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			result, err := client.ListFolder(commandContext(cmd), opts)
			if err != nil {
				return err
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().StringVar(&opts.Parent, "parent", "root", "parent folder ID")
	addDriveListFlags(cmd, &opts.PageSize, &opts.PageToken, &opts.Type, &opts.Trashed)
	return cmd
}

func newDriveFoldersCreateCommand(cfg *appConfig) *cobra.Command {
	var name string
	var parent string
	cmd := &cobra.Command{
		Use:   "create --name NAME",
		Short: "Create a Drive folder",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			folder, err := client.CreateFolder(commandContext(cmd), googledrive.CreateFolderOptions{Name: name, Parent: parent})
			if err != nil {
				return err
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), folder)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "folder name")
	cmd.Flags().StringVar(&parent, "parent", "root", "parent folder ID")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newDriveFoldersTreeCommand(cfg *appConfig) *cobra.Command {
	var opts googledrive.TreeOptions
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show a Drive folder tree",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			tree, err := client.Tree(commandContext(cmd), opts)
			if err != nil {
				return err
			}
			if cfg.jsonOutput {
				return googledrive.WriteJSON(cmd.OutOrStdout(), tree)
			}
			writeTree(cmd.OutOrStdout(), tree, 0)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Parent, "parent", "root", "parent folder ID")
	cmd.Flags().IntVar(&opts.Depth, "depth", 3, "maximum folder depth")
	cmd.Flags().StringVar(&opts.Type, "type", "any", "type filter: any,file,folder,google-doc,google-sheet,google-slide")
	cmd.Flags().BoolVar(&opts.Trashed, "trashed", false, "include trashed files")
	return cmd
}

func newDriveFoldersDownloadCommand(cfg *appConfig) *cobra.Command {
	var opts googledrive.DownloadZipOptions
	cmd := &cobra.Command{
		Use:   "download FOLDER_ID --output folder.zip",
		Short: "Download a Drive folder as a local zip",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Folders = []string{args[0]}
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			if err := client.DownloadZip(commandContext(cmd), opts); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), opts.Output)
			return nil
		},
	}
	addDownloadZipFlags(cmd, &opts)
	_ = cmd.Flags().MarkHidden("file")
	_ = cmd.Flags().MarkHidden("folder")
	return cmd
}

func newDrivePermissionsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "permissions", Short: "Manage Drive permissions"}
	cmd.AddCommand(newDrivePermissionsListCommand(cfg))
	cmd.AddCommand(newDrivePermissionsCreateCommand(cfg))
	cmd.AddCommand(newDrivePermissionsDeleteCommand(cfg))
	return cmd
}

func newDrivePermissionsListCommand(cfg *appConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "list FILE_ID",
		Short: "List Drive permissions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			perms, err := client.ListPermissions(commandContext(cmd), args[0])
			if err != nil {
				return err
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), perms)
		},
	}
}

func newDrivePermissionsCreateCommand(cfg *appConfig) *cobra.Command {
	var email string
	var role string
	cmd := &cobra.Command{
		Use:   "create FILE_ID --email person@example.com --role reader",
		Short: "Create a Drive permission",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			perm, err := client.CreatePermission(commandContext(cmd), googledrive.PermissionCreateOptions{FileID: args[0], Email: email, Role: role})
			if err != nil {
				return err
			}
			return googledrive.WriteJSON(cmd.OutOrStdout(), perm)
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "email address to share with")
	cmd.Flags().StringVar(&role, "role", "reader", "permission role: reader,commenter,writer")
	_ = cmd.MarkFlagRequired("email")
	return cmd
}

func newDrivePermissionsDeleteCommand(cfg *appConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "delete FILE_ID PERMISSION_ID",
		Short: "Delete a Drive permission",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledrive.NewClient(commandContext(cmd), cfg.driveConfig())
			if err != nil {
				return err
			}
			if err := client.DeletePermission(commandContext(cmd), args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted permission %s\n", args[1])
			return nil
		},
	}
}

func addDriveListFlags(cmd *cobra.Command, pageSize *int64, pageToken, typ *string, trashed *bool) {
	cmd.Flags().Int64Var(pageSize, "page-size", 50, "page size")
	cmd.Flags().StringVar(pageToken, "page-token", "", "next page token")
	cmd.Flags().StringVar(typ, "type", "any", "type filter: any,file,folder,google-doc,google-sheet,google-slide")
	cmd.Flags().BoolVar(trashed, "trashed", false, "include trashed files")
}

func addDownloadZipFlags(cmd *cobra.Command, opts *googledrive.DownloadZipOptions) {
	cmd.Flags().StringArrayVar(&opts.Files, "file", nil, "file ID to include; repeat for multiple")
	cmd.Flags().StringArrayVar(&opts.Folders, "folder", nil, "folder ID to include recursively; repeat for multiple")
	cmd.Flags().StringVar(&opts.Output, "output", "", "output zip path")
	cmd.Flags().StringVar(&opts.DocsFormat, "docs-format", "docx", "Google Docs export format: docx,pdf,txt,html")
	cmd.Flags().StringVar(&opts.SheetsFormat, "sheets-format", "xlsx", "Google Sheets export format: xlsx,csv")
	cmd.Flags().StringVar(&opts.SlidesFormat, "slides-format", "pptx", "Google Slides export format: pptx,pdf")
	cmd.Flags().BoolVar(&opts.PreservePaths, "preserve-paths", true, "preserve folder paths in the zip")
	cmd.Flags().BoolVar(&opts.Flatten, "flatten", false, "flatten all files into the zip root")
	_ = cmd.MarkFlagRequired("output")
}

func parseDriveUpdateFields(rawFields []string) ([]googledrive.UpdateField, error) {
	fields := make([]googledrive.UpdateField, 0, len(rawFields))
	for _, raw := range rawFields {
		name, value, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("field update %q must use name=value", raw)
		}
		fields = append(fields, googledrive.UpdateField{Field: strings.TrimSpace(name), Value: strings.TrimSpace(value)})
	}
	return fields, nil
}

func writeTree(w interface{ Write([]byte) (int, error) }, node *googledrive.TreeNode, depth int) {
	if node == nil || node.File == nil {
		return
	}
	prefix := strings.Repeat("  ", depth)
	fmt.Fprintf(w, "%s%s\t%s\t%s\n", prefix, node.File.Name, node.File.Id, node.File.MimeType)
	for _, child := range node.Children {
		writeTree(w, child, depth+1)
	}
}
