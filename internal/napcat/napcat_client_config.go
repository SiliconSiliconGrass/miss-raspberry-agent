package napcat

type NapcatClientConfig struct {
	WebSocketURL  string
	AccessToken   string
	NickName      []string
	CommandPrefix string
	SuperUsers    []int64
}
