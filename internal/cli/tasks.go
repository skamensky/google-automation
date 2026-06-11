package cli

import (
	"fmt"
	"strings"

	"github.com/skamensky/google-automation/internal/googletasks"
	"github.com/spf13/cobra"
)

func newTasksCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "Manage Google Tasks",
	}
	cmd.AddCommand(newTasksListsCommand(cfg))
	cmd.AddCommand(newTasksListCommand(cfg))
	cmd.AddCommand(newTasksGetCommand(cfg))
	cmd.AddCommand(newTasksCreateCommand(cfg))
	cmd.AddCommand(newTasksUpdateCommand(cfg))
	cmd.AddCommand(newTasksCompleteCommand(cfg, true))
	cmd.AddCommand(newTasksCompleteCommand(cfg, false))
	cmd.AddCommand(newTasksMoveCommand(cfg))
	cmd.AddCommand(newTasksDeleteCommand(cfg))
	cmd.AddCommand(newTasksClearCompletedCommand(cfg))
	return cmd
}

func newTasksListsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "lists", Short: "Manage Google Task lists"}
	cmd.AddCommand(newTasksListsListCommand(cfg))
	cmd.AddCommand(newTasksListsCreateCommand(cfg))
	cmd.AddCommand(newTasksListsGetCommand(cfg))
	cmd.AddCommand(newTasksListsUpdateCommand(cfg))
	cmd.AddCommand(newTasksListsDeleteCommand(cfg))
	return cmd
}

func newTasksListsListCommand(cfg *appConfig) *cobra.Command {
	var pageSize int64
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Google Task lists",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			result, err := client.ListTaskLists(commandContext(cmd), googletasks.ListTaskListsOptions{PageSize: pageSize, PageToken: pageToken})
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().Int64Var(&pageSize, "page-size", 20, "page size, max 100")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "next page token")
	return cmd
}

func newTasksListsCreateCommand(cfg *appConfig) *cobra.Command {
	var title string
	cmd := &cobra.Command{
		Use:   "create --title TITLE",
		Short: "Create a Google Task list",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			list, err := client.CreateTaskList(commandContext(cmd), title)
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), list)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "task list title")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newTasksListsGetCommand(cfg *appConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "get TASKLIST_ID",
		Short: "Get a Google Task list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			list, err := client.GetTaskList(commandContext(cmd), args[0])
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), list)
		},
	}
}

func newTasksListsUpdateCommand(cfg *appConfig) *cobra.Command {
	var rawFields []string
	cmd := &cobra.Command{
		Use:   "update TASKLIST_ID --field title=...",
		Short: "Update a Google Task list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fields, err := parseTaskListFields(rawFields)
			if err != nil {
				return err
			}
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			list, err := client.UpdateTaskList(commandContext(cmd), args[0], fields)
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), list)
		},
	}
	cmd.Flags().StringArrayVar(&rawFields, "field", nil, "field update as name=value")
	_ = cmd.MarkFlagRequired("field")
	return cmd
}

func newTasksListsDeleteCommand(cfg *appConfig) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete TASKLIST_ID --yes",
		Short: "Delete a Google Task list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing to delete without --yes")
			}
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			if err := client.DeleteTaskList(commandContext(cmd), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted task list %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm task list deletion")
	return cmd
}

func newTasksListCommand(cfg *appConfig) *cobra.Command {
	var opts googletasks.ListTasksOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Google Tasks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			result, err := client.ListTasks(commandContext(cmd), opts)
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), result)
		},
	}
	addTasklistFlag(cmd, &opts.TasklistID)
	addTaskListFilterFlags(cmd, &opts)
	return cmd
}

func newTasksGetCommand(cfg *appConfig) *cobra.Command {
	var tasklistID string
	cmd := &cobra.Command{
		Use:   "get TASK_ID",
		Short: "Get a Google Task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			task, err := client.GetTask(commandContext(cmd), tasklistID, args[0])
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), task)
		},
	}
	addTasklistFlag(cmd, &tasklistID)
	return cmd
}

func newTasksCreateCommand(cfg *appConfig) *cobra.Command {
	var opts googletasks.CreateTaskOptions
	cmd := &cobra.Command{
		Use:   "create --title TITLE",
		Short: "Create a Google Task",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			task, err := client.CreateTask(commandContext(cmd), opts)
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), task)
		},
	}
	addTasklistFlag(cmd, &opts.TasklistID)
	cmd.Flags().StringVar(&opts.Title, "title", "", "task title")
	cmd.Flags().StringVar(&opts.Notes, "notes", "", "task notes")
	cmd.Flags().StringVar(&opts.Due, "due", "", "due date, YYYY-MM-DD or RFC3339; Tasks API stores date only")
	cmd.Flags().StringVar(&opts.Parent, "parent", "", "parent task ID for subtask creation")
	cmd.Flags().StringVar(&opts.Previous, "previous", "", "previous sibling task ID")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newTasksUpdateCommand(cfg *appConfig) *cobra.Command {
	var tasklistID string
	var rawFields []string
	cmd := &cobra.Command{
		Use:   "update TASK_ID --field title=...",
		Short: "Update a Google Task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fields, err := parseTaskFields(rawFields)
			if err != nil {
				return err
			}
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			task, err := client.UpdateTask(commandContext(cmd), googletasks.UpdateTaskOptions{
				TasklistID: tasklistID,
				TaskID:     args[0],
				Fields:     fields,
			})
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), task)
		},
	}
	addTasklistFlag(cmd, &tasklistID)
	cmd.Flags().StringArrayVar(&rawFields, "field", nil, "field update as name=value; supported: title,notes,due,status")
	_ = cmd.MarkFlagRequired("field")
	return cmd
}

func newTasksCompleteCommand(cfg *appConfig, complete bool) *cobra.Command {
	var tasklistID string
	use := "complete TASK_ID"
	short := "Mark a Google Task complete"
	if !complete {
		use = "uncomplete TASK_ID"
		short = "Mark a Google Task incomplete"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			var task any
			if complete {
				task, err = client.CompleteTask(commandContext(cmd), tasklistID, args[0])
			} else {
				task, err = client.UncompleteTask(commandContext(cmd), tasklistID, args[0])
			}
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), task)
		},
	}
	addTasklistFlag(cmd, &tasklistID)
	return cmd
}

func newTasksMoveCommand(cfg *appConfig) *cobra.Command {
	var opts googletasks.MoveTaskOptions
	cmd := &cobra.Command{
		Use:   "move TASK_ID",
		Short: "Move or reorder a Google Task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.TopLevel && opts.Parent != "" {
				return fmt.Errorf("--top-level and --parent cannot both be set")
			}
			opts.TaskID = args[0]
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			task, err := client.MoveTask(commandContext(cmd), opts)
			if err != nil {
				return err
			}
			return googletasks.WriteJSON(cmd.OutOrStdout(), task)
		},
	}
	addTasklistFlag(cmd, &opts.TasklistID)
	cmd.Flags().StringVar(&opts.DestinationTasklist, "destination-tasklist", "", "destination task list ID for cross-list moves")
	cmd.Flags().StringVar(&opts.Parent, "parent", "", "new parent task ID")
	cmd.Flags().StringVar(&opts.Previous, "previous", "", "new previous sibling task ID")
	cmd.Flags().BoolVar(&opts.TopLevel, "top-level", false, "move task to top level")
	return cmd
}

func newTasksDeleteCommand(cfg *appConfig) *cobra.Command {
	var tasklistID string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete TASK_ID --yes",
		Short: "Delete a Google Task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing to delete without --yes")
			}
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			if err := client.DeleteTask(commandContext(cmd), tasklistID, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted task %s\n", args[0])
			return nil
		},
	}
	addTasklistFlag(cmd, &tasklistID)
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm task deletion")
	return cmd
}

func newTasksClearCompletedCommand(cfg *appConfig) *cobra.Command {
	var tasklistID string
	var yes bool
	cmd := &cobra.Command{
		Use:   "clear-completed --yes",
		Short: "Clear completed tasks from a task list",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes {
				return fmt.Errorf("refusing to clear completed tasks without --yes")
			}
			client, err := googletasks.NewClient(commandContext(cmd), cfg.tasksConfig())
			if err != nil {
				return err
			}
			if err := client.ClearCompleted(commandContext(cmd), tasklistID); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cleared completed tasks")
			return nil
		},
	}
	addTasklistFlag(cmd, &tasklistID)
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm clearing completed tasks")
	return cmd
}

func addTasklistFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "tasklist", "@default", "task list ID, or @default")
}

func addTaskListFilterFlags(cmd *cobra.Command, opts *googletasks.ListTasksOptions) {
	cmd.Flags().BoolVar(&opts.ShowCompleted, "show-completed", true, "show completed tasks")
	cmd.Flags().BoolVar(&opts.ShowDeleted, "show-deleted", false, "show deleted tasks")
	cmd.Flags().BoolVar(&opts.ShowHidden, "show-hidden", true, "show hidden tasks")
	cmd.Flags().StringVar(&opts.DueMin, "due-min", "", "lower bound for due date, RFC3339")
	cmd.Flags().StringVar(&opts.DueMax, "due-max", "", "upper bound for due date, RFC3339")
	cmd.Flags().StringVar(&opts.CompletedMin, "completed-min", "", "lower bound for completion date, RFC3339")
	cmd.Flags().StringVar(&opts.CompletedMax, "completed-max", "", "upper bound for completion date, RFC3339")
	cmd.Flags().StringVar(&opts.UpdatedMin, "updated-min", "", "lower bound for updated time, RFC3339")
	cmd.Flags().Int64Var(&opts.PageSize, "page-size", 20, "page size, max 100")
	cmd.Flags().StringVar(&opts.PageToken, "page-token", "", "next page token")
}

func parseTaskListFields(rawFields []string) ([]googletasks.TaskListFieldUpdate, error) {
	fields := make([]googletasks.TaskListFieldUpdate, 0, len(rawFields))
	for _, raw := range rawFields {
		field, value, err := googletasks.ParseField(raw)
		if err != nil {
			return nil, err
		}
		fields = append(fields, googletasks.TaskListFieldUpdate{Field: field, Value: value})
	}
	return fields, nil
}

func parseTaskFields(rawFields []string) ([]googletasks.TaskFieldUpdate, error) {
	fields := make([]googletasks.TaskFieldUpdate, 0, len(rawFields))
	for _, raw := range rawFields {
		field, value, err := googletasks.ParseField(raw)
		if err != nil {
			return nil, err
		}
		if field == "due" && strings.TrimSpace(value) == "" {
			value = ""
		}
		fields = append(fields, googletasks.TaskFieldUpdate{Field: field, Value: value})
	}
	return fields, nil
}
