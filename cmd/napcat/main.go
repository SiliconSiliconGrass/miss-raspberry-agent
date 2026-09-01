package main

import (
	"log"
	"time"

	"github.com/joho/godotenv"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/driver"
)

func main() {
	// zero.OnCommand("hello").
	// 	Handle(func(ctx *zero.Ctx) {
	// 		ctx.Send("world")
	// 	})
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	zero.OnCommand("*").
		Handle(func(ctx *zero.Ctx) {
			ctx.Send("world")
		})

	zero.OnMessage(func(ctx *zero.Ctx) bool {
		// return ctx.Event.Message.String() == "hello"
		return true
	}).Handle(func(ctx *zero.Ctx) {
		ctx.Send("hi~")
	})

	zero.Run(&zero.Config{
		NickName:      []string{"bot"},
		CommandPrefix: "/",
		SuperUsers:    []int64{123},
		Driver: []zero.Driver{
			// // 正向 WS
			driver.NewWebSocketClient("ws://127.0.0.1:3001", "dqz7TxqGlU3X-dQU"),
			// // 反向 WS
			// driver.NewWebSocketServer(16, "ws://localhost:6199", ""),
			// // HTTP
			// driver.NewHTTPClient("http://127.0.0.1:6701", "", "http://127.0.0.1:3001", "123"),
		},
	})

	i := 0
	for {

		if i == 0 {
			zero.RangeBot(func(_ int64, ctx *zero.Ctx) bool {
				ctx.SendPrivateMessage(123, "心跳：bot 在线")
				return false
			})
		}

		i += 1
		time.Sleep(time.Second)
	}

}
