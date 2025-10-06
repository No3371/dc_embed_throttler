package bot

import (
	"log"
	"runtime/debug"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
)

type Middleware[S any] func(e *gateway.InteractionCreateEvent, state *S, next ...Middleware[S]) error

func LoggingMiddleware[S any](e *gateway.InteractionCreateEvent, state *S, next ...Middleware[S]) error {
	sender := int64(e.SenderID())
	channelId := int64(e.ChannelID)
	// sb := strings.Builder{}
	// sb.Grow(32)
	// sb.WriteString(strconv.FormatInt(sender, 10))
	// sb.WriteString(" in ")
	// sb.WriteString(strconv.FormatInt(channelId, 10))
	// sb.WriteString(": ")
	// switch data := e.Data.(type) {
	// case *discord.PingInteraction:
	// 	sb.WriteString("ping")
	// case *discord.CommandInteraction:
	// 	sb.WriteString("command")
	// 	sb.WriteString(" ")
	// 	sb.WriteString(data.Name)
	// case *discord.ButtonInteraction:
	// 	sb.WriteString("button")
	// 	sb.WriteString(" ")
	// 	sb.WriteString(string(data.ID()))
	// default:
	// 	sb.WriteString("unknown")
	// }

	if len(next) > 0 {
		t := time.Now()
		log.Printf("\n-> %d in %d", sender, channelId)
		err := next[0](e, state, next[1:]...)
		log.Printf("\n<- %d in %d %v", sender, channelId, time.Since(t))
		return err
	}
	return nil
}

func PanicRecoveryMiddleware[S any](e *gateway.InteractionCreateEvent, state *S, next ...Middleware[S]) error {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC: %+v\n%s", r, debug.Stack())
		}
	}()
	if len(next) > 0 {
		return next[0](e, state, next[1:]...)
	}
	return nil
}
