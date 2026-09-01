package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
)

type WeatherInput struct {
	City string `json:"city" jsonschema:"required,description=要查询天气的城市"`
}

type WeatherOutput struct {
	City    string `json:"city"`
	Weather string `json:"weather"`
}

func QueryWeather(ctx context.Context, in *WeatherInput) (*WeatherOutput, error) {
	// 这里可以调真实 API，先写死演示
	return &WeatherOutput{
		City:    in.City,
		Weather: "雨，32℃",
	}, nil
}

func main() {

	// err := godotenv.Load("")
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// fmt.Println(os.Getenv("MODEL_NAME"))

	modelCtx := context.Background()

	// chatModel, err := model.New(ctx, cfg.Model)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	weatherTool, err := utils.InferTool(
		"query_weather", // tool name，LLM 靠这个调用
		"查询指定城市的当前天气",   // tool desc，写给 LLM 看的
		QueryWeather,    // 上面那个函数
	)
	if err != nil {
		panic(err)
	}
	// weatherToolCtx := context.Background()
	// toolsNode, err := compose.NewToolNode(weatherToolCtx, &compose.ToolsNodeConfig{
	// 	Tools: []tool.BaseTool{weatherTool},
	// 	// ExecuteSequentially: true, // 多个 tool call 时按顺序执行
	// })

	chatModel, err := einoopenai.NewChatModel(modelCtx, &einoopenai.ChatModelConfig{
		APIKey:  os.Getenv("MODEL_API_KEY"),
		BaseURL: os.Getenv("MODEL_BASE_URL"),
		Model:   os.Getenv("MODEL_NAME"),
		// ResponseFormat: &einoopenai.ChatCompletionResponseFormat{
		// 	Type: einoopenai.ChatCompletionResponseFormatTypeJSONObject,
		// },
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Model type: %T\n", chatModel)

	agentCtx := context.Background()
	agent, _ := adk.NewChatModelAgent(agentCtx, &adk.ChatModelAgentConfig{
		Name:        "assistant",
		Description: "A helpful assistant.",
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				// Tools: []tool.BaseTool{weatherTool, calculatorTool},
				Tools: []tool.BaseTool{weatherTool},
			},
		},
	})

	runnerCtx := context.Background()
	runner := adk.NewRunner(runnerCtx, adk.RunnerConfig{Agent: agent})
	iter := runner.Query(runnerCtx, "What's the weather today in beijing?")
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		data, err := json.Marshal(event)
		if err != nil {
			log.Fatal((err))
		}

		fmt.Println(string(data))
		// process agent events (model outputs, tool calls, etc.)

		adk.NewEventSenderToolWrapper()
	}
}
