package todo_list

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type TodoListInput struct {
	Action  string `json:"action" jsonschema:"enum=list,enum=add,enum=complete,description=要执行的操作：list=查看全部待办，add=新增待办，complete=完成并立即删除指定待办"`
	Content string `json:"content,omitempty" jsonschema:"description=add 操作时要新增的待办内容"`
	ID      string `json:"id,omitempty" jsonschema:"description=complete 操作时要完成并删除的待办 id（来自 list 结果）"`
}

type TodoListOutput struct {
	Items   []Item `json:"items,omitempty"`
	Message string `json:"message,omitempty"`
}

// RunTodoList 执行 list/add/complete 三种待办操作。
func RunTodoList(ctx context.Context, store *Store, in *TodoListInput) (*TodoListOutput, error) {
	switch in.Action {
	case "list":
		return &TodoListOutput{Items: store.List()}, nil
	case "add":
		if in.Content == "" {
			return nil, errors.New("todo_list: content is required for action add")
		}
		item := store.Add(Item{Content: in.Content})
		return &TodoListOutput{
			Items:   store.List(),
			Message: fmt.Sprintf("已添加待办 %s", item.ID),
		}, nil
	case "complete":
		if in.ID == "" {
			return nil, errors.New("todo_list: id is required for action complete")
		}
		item, ok := store.Complete(in.ID)
		if !ok {
			return &TodoListOutput{
				Items:   store.List(),
				Message: fmt.Sprintf("待办 %s 不存在，可能已被清除", in.ID),
			}, nil
		}
		return &TodoListOutput{
			Items:   store.List(),
			Message: fmt.Sprintf("已完成并删除待办 %s", item.ID),
		}, nil
	default:
		return nil, fmt.Errorf("todo_list: unknown action %q (expected list/add/complete)", in.Action)
	}
}

// NewTodoListTool 构造待办列表工具。
func NewTodoListTool(store *Store) tool.BaseTool {
	fn := func(ctx context.Context, in *TodoListInput) (*TodoListOutput, error) {
		return RunTodoList(ctx, store, in)
	}
	t, err := utils.InferTool(
		"todo_list",
		"待办列表管理：list 查看全部待办，add 新增待办，complete 完成并立即删除指定待办（传入其 id）。"+
			"注意：该列表仅用于存放需要立即处理的任务（例如刚收到的待回复消息），禁止放入长期提醒或定时任务；"+
			"任务处理完成后必须立即用 complete 清除对应待办，不要遗留已完成的任务。",
		fn,
	)
	if err != nil {
		log.Fatal(err)
	}
	return t
}
