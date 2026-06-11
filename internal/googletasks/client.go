package googletasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/skamensky/google-automation/internal/cache"
	"github.com/skamensky/google-automation/internal/googleauth"
	"google.golang.org/api/option"
	tasks "google.golang.org/api/tasks/v1"
)

type Config struct {
	CredentialsFile string
	TokenFile       string
	NoBrowser       bool
	Cache           cache.Config
}

type Client struct {
	service    *tasks.Service
	httpClient *http.Client
	cache      cache.Store
}

type TaskListResult struct {
	Items         []*tasks.TaskList `json:"items"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
}

type TaskResult struct {
	Items         []*tasks.Task `json:"items"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

type ListTaskListsOptions struct {
	PageSize  int64
	PageToken string
}

type TaskListFieldUpdate struct {
	Field string
	Value string
}

type ListTasksOptions struct {
	TasklistID    string
	ShowCompleted bool
	ShowDeleted   bool
	ShowHidden    bool
	DueMin        string
	DueMax        string
	CompletedMin  string
	CompletedMax  string
	UpdatedMin    string
	PageSize      int64
	PageToken     string
}

type CreateTaskOptions struct {
	TasklistID string
	Title      string
	Notes      string
	Due        string
	Parent     string
	Previous   string
}

type TaskFieldUpdate struct {
	Field string
	Value string
}

type UpdateTaskOptions struct {
	TasklistID string
	TaskID     string
	Fields     []TaskFieldUpdate
}

type MoveTaskOptions struct {
	TasklistID          string
	TaskID              string
	DestinationTasklist string
	Parent              string
	Previous            string
	TopLevel            bool
}

func Scopes() []string {
	return []string{tasks.TasksScope}
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
	service, err := tasks.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create tasks service: %w", err)
	}
	return &Client{service: service, httpClient: httpClient, cache: cache.NewStore(cfg.Cache)}, nil
}

func (c *Client) ListTaskLists(ctx context.Context, opts ListTaskListsOptions) (TaskListResult, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	call := c.service.Tasklists.List().MaxResults(opts.PageSize).Context(ctx)
	if opts.PageToken != "" {
		call.PageToken(opts.PageToken)
	}
	resp, err := call.Do()
	if err != nil {
		return TaskListResult{}, fmt.Errorf("list task lists: %w", err)
	}
	return TaskListResult{Items: resp.Items, NextPageToken: resp.NextPageToken}, nil
}

func (c *Client) ResolveTasklist(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id != "" && id != "@default" {
		return id, nil
	}
	lists, err := c.ListTaskLists(ctx, ListTaskListsOptions{PageSize: 1})
	if err != nil {
		return "", err
	}
	if len(lists.Items) == 0 {
		return "", errors.New("no task lists found")
	}
	return lists.Items[0].Id, nil
}

func (c *Client) GetTaskList(ctx context.Context, id string) (*tasks.TaskList, error) {
	resolved, err := c.ResolveTasklist(ctx, id)
	if err != nil {
		return nil, err
	}
	list, err := c.service.Tasklists.Get(resolved).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get task list %q: %w", resolved, err)
	}
	return list, nil
}

func (c *Client) CreateTaskList(ctx context.Context, title string) (*tasks.TaskList, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("title is required")
	}
	list, err := c.service.Tasklists.Insert(&tasks.TaskList{Title: title}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("create task list: %w", err)
	}
	return list, c.invalidate(ctx)
}

func (c *Client) UpdateTaskList(ctx context.Context, id string, fields []TaskListFieldUpdate) (*tasks.TaskList, error) {
	resolved, err := c.ResolveTasklist(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, errors.New("at least one --field is required")
	}
	list := &tasks.TaskList{}
	for _, field := range fields {
		switch field.Field {
		case "title":
			list.Title = field.Value
		default:
			return nil, fmt.Errorf("unsupported task list field %q; supported fields: title", field.Field)
		}
	}
	updated, err := c.service.Tasklists.Patch(resolved, list).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("update task list %q: %w", resolved, err)
	}
	return updated, c.invalidate(ctx)
}

func (c *Client) DeleteTaskList(ctx context.Context, id string) error {
	resolved, err := c.ResolveTasklist(ctx, id)
	if err != nil {
		return err
	}
	if err := c.service.Tasklists.Delete(resolved).Context(ctx).Do(); err != nil {
		return fmt.Errorf("delete task list %q: %w", resolved, err)
	}
	return c.invalidate(ctx)
}

func (c *Client) ListTasks(ctx context.Context, opts ListTasksOptions) (TaskResult, error) {
	tasklistID, err := c.ResolveTasklist(ctx, opts.TasklistID)
	if err != nil {
		return TaskResult{}, err
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	call := c.service.Tasks.List(tasklistID).
		MaxResults(opts.PageSize).
		ShowCompleted(opts.ShowCompleted).
		ShowDeleted(opts.ShowDeleted).
		ShowHidden(opts.ShowHidden).
		Context(ctx)
	if opts.PageToken != "" {
		call.PageToken(opts.PageToken)
	}
	if opts.DueMin != "" {
		call.DueMin(opts.DueMin)
	}
	if opts.DueMax != "" {
		call.DueMax(opts.DueMax)
	}
	if opts.CompletedMin != "" {
		call.CompletedMin(opts.CompletedMin)
	}
	if opts.CompletedMax != "" {
		call.CompletedMax(opts.CompletedMax)
	}
	if opts.UpdatedMin != "" {
		call.UpdatedMin(opts.UpdatedMin)
	}
	resp, err := call.Do()
	if err != nil {
		return TaskResult{}, fmt.Errorf("list tasks: %w", err)
	}
	return TaskResult{Items: resp.Items, NextPageToken: resp.NextPageToken}, nil
}

func (c *Client) GetTask(ctx context.Context, tasklistID, taskID string) (*tasks.Task, error) {
	tasklistID, err := c.ResolveTasklist(ctx, tasklistID)
	if err != nil {
		return nil, err
	}
	task, err := c.service.Tasks.Get(tasklistID, taskID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get task %q: %w", taskID, err)
	}
	return task, nil
}

func (c *Client) CreateTask(ctx context.Context, opts CreateTaskOptions) (*tasks.Task, error) {
	tasklistID, err := c.ResolveTasklist(ctx, opts.TasklistID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.Title) == "" {
		return nil, errors.New("title is required")
	}
	task := &tasks.Task{Title: opts.Title, Notes: opts.Notes}
	if opts.Due != "" {
		task.Due = normalizeDue(opts.Due)
	}
	call := c.service.Tasks.Insert(tasklistID, task).Context(ctx)
	if opts.Parent != "" {
		call.Parent(opts.Parent)
	}
	if opts.Previous != "" {
		call.Previous(opts.Previous)
	}
	created, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}
	return created, c.invalidate(ctx)
}

func (c *Client) UpdateTask(ctx context.Context, opts UpdateTaskOptions) (*tasks.Task, error) {
	tasklistID, err := c.ResolveTasklist(ctx, opts.TasklistID)
	if err != nil {
		return nil, err
	}
	if len(opts.Fields) == 0 {
		return nil, errors.New("at least one --field is required")
	}
	task := &tasks.Task{}
	for _, field := range opts.Fields {
		if err := applyTaskField(task, field); err != nil {
			return nil, err
		}
	}
	updated, err := c.service.Tasks.Patch(tasklistID, opts.TaskID, task).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("update task %q: %w", opts.TaskID, err)
	}
	return updated, c.invalidate(ctx)
}

func (c *Client) CompleteTask(ctx context.Context, tasklistID, taskID string) (*tasks.Task, error) {
	return c.UpdateTask(ctx, UpdateTaskOptions{
		TasklistID: tasklistID,
		TaskID:     taskID,
		Fields: []TaskFieldUpdate{
			{Field: "status", Value: "completed"},
		},
	})
}

func (c *Client) UncompleteTask(ctx context.Context, tasklistID, taskID string) (*tasks.Task, error) {
	return c.UpdateTask(ctx, UpdateTaskOptions{
		TasklistID: tasklistID,
		TaskID:     taskID,
		Fields: []TaskFieldUpdate{
			{Field: "status", Value: "needsAction"},
		},
	})
}

func (c *Client) MoveTask(ctx context.Context, opts MoveTaskOptions) (*tasks.Task, error) {
	tasklistID, err := c.ResolveTasklist(ctx, opts.TasklistID)
	if err != nil {
		return nil, err
	}
	if opts.DestinationTasklist != "" {
		return c.moveTaskREST(ctx, tasklistID, opts)
	}
	call := c.service.Tasks.Move(tasklistID, opts.TaskID).Context(ctx)
	if opts.Parent != "" && !opts.TopLevel {
		call.Parent(opts.Parent)
	}
	if opts.Previous != "" {
		call.Previous(opts.Previous)
	}
	moved, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("move task %q: %w", opts.TaskID, err)
	}
	return moved, c.invalidate(ctx)
}

func (c *Client) moveTaskREST(ctx context.Context, tasklistID string, opts MoveTaskOptions) (*tasks.Task, error) {
	destinationTasklist, err := c.ResolveTasklist(ctx, opts.DestinationTasklist)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("https://tasks.googleapis.com/tasks/v1/lists/%s/tasks/%s/move",
		url.PathEscape(tasklistID),
		url.PathEscape(opts.TaskID),
	)
	values := url.Values{}
	values.Set("destinationTasklist", destinationTasklist)
	if opts.Parent != "" && !opts.TopLevel {
		values.Set("parent", opts.Parent)
	}
	if opts.Previous != "" {
		values.Set("previous", opts.Previous)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build move task request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("move task %q: %w", opts.TaskID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("move task %q: google tasks returned %s: %s", opts.TaskID, resp.Status, strings.TrimSpace(string(body)))
	}
	var moved tasks.Task
	if err := json.NewDecoder(resp.Body).Decode(&moved); err != nil {
		return nil, fmt.Errorf("decode moved task: %w", err)
	}
	return &moved, c.invalidate(ctx)
}

func (c *Client) DeleteTask(ctx context.Context, tasklistID, taskID string) error {
	tasklistID, err := c.ResolveTasklist(ctx, tasklistID)
	if err != nil {
		return err
	}
	if err := c.service.Tasks.Delete(tasklistID, taskID).Context(ctx).Do(); err != nil {
		return fmt.Errorf("delete task %q: %w", taskID, err)
	}
	return c.invalidate(ctx)
}

func (c *Client) ClearCompleted(ctx context.Context, tasklistID string) error {
	tasklistID, err := c.ResolveTasklist(ctx, tasklistID)
	if err != nil {
		return err
	}
	if err := c.service.Tasks.Clear(tasklistID).Context(ctx).Do(); err != nil {
		return fmt.Errorf("clear completed tasks: %w", err)
	}
	return c.invalidate(ctx)
}

func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func SupportedTaskFields() []string {
	return []string{"due", "notes", "status", "title"}
}

func ParseField(raw string) (field, value string, err error) {
	field, value, ok := strings.Cut(strings.TrimSpace(raw), "=")
	if !ok || strings.TrimSpace(field) == "" {
		return "", "", fmt.Errorf("field update %q must use name=value", raw)
	}
	return strings.TrimSpace(field), strings.TrimSpace(value), nil
}

func applyTaskField(task *tasks.Task, field TaskFieldUpdate) error {
	switch field.Field {
	case "title":
		task.Title = field.Value
	case "notes":
		task.Notes = field.Value
	case "due":
		task.Due = normalizeDue(field.Value)
		if field.Value == "" {
			task.NullFields = append(task.NullFields, "Due")
		}
	case "status":
		if field.Value != "needsAction" && field.Value != "completed" {
			return fmt.Errorf("invalid status %q; valid values: needsAction,completed", field.Value)
		}
		task.Status = field.Value
	default:
		return fmt.Errorf("unsupported task field %q; supported fields: %s", field.Field, strings.Join(SupportedTaskFields(), ","))
	}
	return nil
}

func normalizeDue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return value
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.Format("2006-01-02T00:00:00.000Z")
	}
	return value
}

func (c *Client) invalidate(ctx context.Context) error {
	return c.cache.DeleteNamespace(ctx, "tasks")
}

func SortTasksByPosition(items []*tasks.Task) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Position < items[j].Position
	})
}

func ParsePageSize(value string) (int64, error) {
	if value == "" {
		return 20, nil
	}
	return strconv.ParseInt(value, 10, 64)
}
