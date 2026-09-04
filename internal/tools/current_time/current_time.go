// Package current_time provides a tool for getting the current Beijing time (Asia/Shanghai).
package current_time

import (
	"context"
	"log"
	"time"
	_ "time/tzdata" // Ensure Asia/Shanghai timezone data is available

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

var beijing = loadLocation()

func loadLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return loc
}

type CurrentTimeInput struct{}

type CurrentTimeOutput struct {
	Time     string `json:"time"`
	Date     string `json:"date"`
	Weekday  string `json:"weekday"`
	Unix     int64  `json:"unix"`
	Timezone string `json:"timezone"`
}

// CurrentTime returns the current Beijing time.
func CurrentTime(ctx context.Context, in *CurrentTimeInput) (*CurrentTimeOutput, error) {
	now := time.Now().In(beijing)
	return &CurrentTimeOutput{
		Time:     now.Format("2006-01-02 15:04:05"),
		Date:     now.Format("2006-01-02"),
		Weekday:  weekdayCN(now.Weekday()),
		Unix:     now.Unix(),
		Timezone: "Asia/Shanghai (UTC+8)",
	}, nil
}

func weekdayCN(d time.Weekday) string {
	switch d {
	case time.Monday:
		return "星期一"
	case time.Tuesday:
		return "星期二"
	case time.Wednesday:
		return "星期三"
	case time.Thursday:
		return "星期四"
	case time.Friday:
		return "星期五"
	case time.Saturday:
		return "星期六"
	case time.Sunday:
		return "星期日"
	}
	return d.String()
}

// NewCurrentTimeTool constructs the "get current time" tool.
func NewCurrentTimeTool() tool.BaseTool {
	t, err := utils.InferTool(
		"current_time",
		"获取当前北京时间（Asia/Shanghai，UTC+8）：返回当前日期、时间、星期和 Unix 时间戳。需要知道当前时间、今天是星期几、距某个时间点还有多久时使用。",
		CurrentTime,
	)
	if err != nil {
		log.Fatal(err)
	}
	return t
}
