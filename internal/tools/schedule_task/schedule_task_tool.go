// Package schedule_task 提供定时任务管理工具，供 agent 创建/查看/取消定时任务。
package schedule_task

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"miss-raspberry-agent/internal/scheduler"
)

type ScheduleTaskInput struct {
	Action  string `json:"action" jsonschema:"enum=create,enum=list,enum=cancel,description=要执行的操作：create=创建定时任务，list=查看全部定时任务，cancel=取消指定定时任务"`
	Content string `json:"content,omitempty" jsonschema:"description=create 操作的任务内容：触发时间到达后，这条内容会交给 agent 执行（例如“提醒用户喝水”），应写明触发后要做的具体事情"`
	Type    string `json:"type,omitempty" jsonschema:"enum=once,enum=relative,enum=daily,enum=weekly,enum=monthly,enum=yearly,description=create 操作必填：触发类型。once=指定时间触发一次；relative=从现在起延迟多久后触发一次；daily=每天固定时间；weekly=每周固定星期几；monthly=每月固定几号；yearly=每年固定月日"`

	Datetime string `json:"datetime,omitempty" jsonschema:"description=type=once 时必填：触发时间，格式 2006-01-02 15:04（北京时间），例如 2026-09-01 10:00"`
	Minutes  int    `json:"minutes,omitempty" jsonschema:"description=type=relative 时使用：多少分钟后触发，例如 10"`
	Hours    int    `json:"hours,omitempty" jsonschema:"description=type=relative 时使用：多少小时后触发"`
	Days     int    `json:"days,omitempty" jsonschema:"description=type=relative 时使用：多少天后触发"`

	Weekday    string `json:"weekday,omitempty" jsonschema:"description=type=weekly 时必填：星期几，英文，如 Mon/Tue/Wed/Thu/Fri/Sat/Sun 或 Monday...Sunday（大小写均可）"`
	Time       string `json:"time,omitempty" jsonschema:"description=type=daily/weekly/monthly/yearly 时必填：触发时间，24小时制 HH:MM，如 08:00、12:10"`
	DayOfMonth int    `json:"day_of_month,omitempty" jsonschema:"description=type=monthly 时必填：每月几号（1-31），如 30"`
	Date       string `json:"date,omitempty" jsonschema:"description=type=yearly 时必填：月日，格式 MM-DD，如 04-24"`

	TaskID string `json:"task_id,omitempty" jsonschema:"description=cancel 操作要取消的任务 id（来自 list 结果）"`

	TargetType string `json:"target_type,omitempty" jsonschema:"enum=private,enum=group,description=可选：触发后回复的目标类型（private/group），默认沿用创建任务时的消息来源"`
	TargetID   int64  `json:"target_id,omitempty" jsonschema:"description=可选：触发后回复的目标 QQ 号/群号，默认沿用创建任务时的消息来源"`
}

type ScheduleTaskOutput struct {
	Task    *scheduler.Task  `json:"task,omitempty"`
	Tasks   []scheduler.Task `json:"tasks,omitempty"`
	Message string           `json:"message,omitempty"`
}

// SourceProvider 返回当前正在处理的消息来源（回复目标）。
type SourceProvider func() scheduler.Source

// RunScheduleTask 执行 create/list/cancel 三种定时任务操作。
func RunScheduleTask(ctx context.Context, store *scheduler.Store, current SourceProvider, in *ScheduleTaskInput) (*ScheduleTaskOutput, error) {
	switch in.Action {
	case "create":
		if in.Content == "" {
			return nil, errors.New("schedule_task: content is required for action create")
		}
		src := scheduler.Source{}
		if current != nil {
			src = current()
		}
		if in.TargetType != "" {
			src.TargetType = in.TargetType
		}
		if in.TargetID > 0 {
			src.TargetID = in.TargetID
		}
		if src.TargetType != "" && src.TargetID <= 0 {
			return nil, errors.New("schedule_task: target_id is required when target_type is set")
		}
		sch, err := buildSchedule(in, store.Location())
		if err != nil {
			return nil, err
		}
		task, err := store.Add(in.Content, sch, src)
		if err != nil {
			return nil, err
		}
		return &ScheduleTaskOutput{
			Task:    task,
			Message: fmt.Sprintf("已创建定时任务 %s，触发规则：%s，下次触发：%s", task.ID, task.Schedule, task.NextRunText()),
		}, nil
	case "list":
		return &ScheduleTaskOutput{Tasks: store.List()}, nil
	case "cancel":
		if in.TaskID == "" {
			return nil, errors.New("schedule_task: task_id is required for action cancel")
		}
		t, ok := store.Cancel(in.TaskID)
		if !ok {
			return &ScheduleTaskOutput{Message: fmt.Sprintf("定时任务 %s 不存在，可能已被取消或已触发", in.TaskID)}, nil
		}
		return &ScheduleTaskOutput{
			Task:    &t,
			Message: fmt.Sprintf("已取消定时任务 %s：%s", t.ID, t.Content),
		}, nil
	default:
		return nil, fmt.Errorf("schedule_task: unknown action %q (expected create/list/cancel)", in.Action)
	}
}

// buildSchedule 根据 type 及其对应参数构造调度规则，每个 type 单独校验和解析。
func buildSchedule(in *ScheduleTaskInput, loc *time.Location) (*scheduler.Schedule, error) {
	switch in.Type {
	case "once":
		if in.Datetime == "" {
			return nil, errors.New("schedule_task: datetime is required for type once")
		}
		at, err := scheduler.ParseDateTime(in.Datetime, loc)
		if err != nil {
			return nil, err
		}
		return scheduler.NewOnce(at)
	case "relative":
		if in.Minutes == 0 && in.Hours == 0 && in.Days == 0 {
			return nil, errors.New("schedule_task: at least one of minutes/hours/days is required for type relative")
		}
		if in.Minutes < 0 || in.Hours < 0 || in.Days < 0 {
			return nil, errors.New("schedule_task: minutes/hours/days must be non-negative")
		}
		delay := time.Duration(in.Minutes)*time.Minute +
			time.Duration(in.Hours)*time.Hour +
			time.Duration(in.Days)*24*time.Hour
		return scheduler.NewRelative(delay)
	case "daily":
		return scheduler.NewDaily(in.Time)
	case "weekly":
		return scheduler.NewWeekly(in.Weekday, in.Time)
	case "monthly":
		return scheduler.NewMonthly(in.DayOfMonth, in.Time)
	case "yearly":
		return scheduler.NewYearly(in.Date, in.Time)
	default:
		return nil, fmt.Errorf("schedule_task: unknown type %q (expected once/relative/daily/weekly/monthly/yearly)", in.Type)
	}
}

// NewScheduleTaskTool 构造定时任务管理工具。
func NewScheduleTaskTool(store *scheduler.Store, current SourceProvider) tool.BaseTool {
	fn := func(ctx context.Context, in *ScheduleTaskInput) (*ScheduleTaskOutput, error) {
		return RunScheduleTask(ctx, store, current, in)
	}
	t, err := utils.InferTool(
		"schedule_task",
		"定时任务管理：create 创建定时任务（触发时间到了之后，任务内容会自动交给 agent 执行并回复结果），list 查看全部定时任务，cancel 取消指定任务。"+
			"创建时必须通过 type 指定触发类型，并用对应的 ASCII 参数描述时间：once 用 datetime（如 2026-09-01 10:00）；relative 用 minutes/hours/days（如 10 分钟后就写 minutes=10）；daily 用 time（如 time=\"08:00\"）；weekly 用 weekday（英文星期，如 weekday=\"Sat\"）和 time；monthly 用 day_of_month（如 day_of_month=30）和 time；yearly 用 date（如 date=\"04-24\"）和 time。"+
			"一次 create 只创建一条定时任务：如果用户要求多个星期几或多个时间（例如每周六和每周日12:00和12:10），必须拆成多条定时任务分别创建（周六12:00、周六12:10、周日12:00、周日12:10 共4条）。",
		fn,
	)
	if err != nil {
		log.Fatal(err)
	}
	return t
}
