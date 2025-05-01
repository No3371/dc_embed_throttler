package bot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"runtime/debug"
	"strings"
	"time"

	"github.com/No3371/dc_embed_throttler/config"
	"github.com/No3371/dc_embed_throttler/storage"
	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"github.com/maypok86/otter"
)

type InteractionHandlerState struct {
}

type Bot struct {
	s                  *state.State
	storage            storage.Storage
	config             *config.Config
	interactionHandler Middleware[InteractionHandlerState]
}

func NewBot(cfg *config.Config, store storage.Storage) (*Bot, error) {
	return &Bot{
		s:       state.New("Bot " + cfg.Token),
		storage: store,
		config:  cfg,
	}, nil
}

func (b *Bot) Start(ctx context.Context) error {
	b.s.AddHandler(b.handleMessageCreate)
	// b.s.AddHandler(b.handleMessageEdit)
	b.s.AddHandler(b.handleInteractionCreate)
	b.s.AddHandler(func(m *gateway.ReadyEvent) {
		fmt.Printf("Ready!")
		if b.config.UpdateCommands {

			liveCmds, err := b.s.Commands(discord.AppID(b.s.Ready().Application.ID))
			if err != nil {
				log.Printf("Error getting commands: %v", err)
			}
			for _, cmd := range liveCmds {
				switch cmd.Name {
				case "restore_embeds":
					b.s.DeleteCommand(discord.AppID(b.s.Ready().Application.ID), cmd.ID)
				}
			}

			perms := discord.PermissionManageChannels
			cmds, err := b.s.BulkOverwriteCommands(discord.AppID(b.s.Ready().Application.ID), []api.CreateCommandData{
				{
					Name: "suppress_embeds",
					Type: discord.MessageCommand,
					NameLocalizations: map[discord.Language]string{
						discord.EnglishUS:     "Suppress Embeds",
						discord.ChineseChina:  "抑制嵌入",
						discord.ChineseTaiwan: "抑制嵌入",
						discord.Japanese:      "埋め込みを抑制する",
					},
				},
				{
					Name:                     "toggle_channel",
					Description:              "開關嵌入限流",
					Type:                     discord.ChatInputCommand,
					DefaultMemberPermissions: &perms,
				},
				{
					Name:                     "toggle_suppress_bot",
					Description:              "開關抑制機器人訊息",
					Type:                     discord.ChatInputCommand,
					DefaultMemberPermissions: &perms,
				},
				{
					Name:                     "reset_quota",
					Description:              "重設個人嵌入額度",
					Type:                     discord.ChatInputCommand,
					DefaultMemberPermissions: &perms,
					Options: []discord.CommandOption{
						discord.NewUserOption(
							"user",
							"使用者",
							true,
						),
					},
				},
				{
					Name:                     "set_role_quota",
					Description:              "設定身分組嵌入限流",
					Type:                     discord.ChatInputCommand,
					DefaultMemberPermissions: &perms,
					Options: []discord.CommandOption{
						discord.NewRoleOption(
							"role",
							"身分組",
							true,
						),
						discord.NewIntegerOption(
							"quota",
							"額度",
							true,
						),
						discord.NewIntegerOption(
							"priority",
							"優先度（高者優先採用）",
							true,
						),
					},
				},
				{
					Name:                     "list_role_quotas",
					Description:              "列出所有身分組嵌入限流設定",
					Type:                     discord.ChatInputCommand,
					DefaultMemberPermissions: &perms,
				},
				{
					Name:                     "my_quota",
					Description:              "查看個人嵌入額度",
					Type:                     discord.ChatInputCommand,
				},
			})
			if err != nil {
				log.Printf("Error overwriting commands: %v", err)
			}
			log.Printf("Overwrote %d commands", len(cmds))
		}
	})

	b.s.AddIntents(gateway.IntentGuilds | gateway.IntentGuildMessages | gateway.IntentMessageContent)
	ChanDeferredSuppress = make(chan *gateway.MessageCreateEvent, 32)
	go b.LateSupressLoop()
	return b.s.Open(ctx)
}

func (b *Bot) handleMessageCreate(m *gateway.MessageCreateEvent) {
	enabled, err := b.storage.IsChannelEnabled(uint64(m.ChannelID))
	if err != nil {
		log.Printf("Error checking channel status: %v", err)
		return
	}

	if !enabled && !b.config.DefaultEnabled {
		return
	}

	if len(m.Embeds) == 0 {
		if strings.Contains(m.Content, "http") {
			ChanDeferredSuppress <- m
			log.Printf("Message %d deferred", m.ID)
			return
		}
		log.Printf("Message %d has no embeds and not potential link", m.ID)
		return
	} else {
		b.TrySurpress(m)
	}
}

var ChanDeferredSuppress chan *gateway.MessageCreateEvent

func (b *Bot) LateSupressLoop() {
	for {
		mv := <-ChanDeferredSuppress
		if time.Since(mv.Timestamp.Time()) < time.Millisecond*100 {
			time.Sleep(time.Millisecond * 100)
		}

		msg, err := b.s.Message(mv.ChannelID, mv.ID)
		if err != nil {
			log.Printf("Error getting message: %v", err)
			continue
		}

		if len(msg.Embeds) == 0 {
			continue
		}


		b.TrySurpress(mv)
	}
}

func (b *Bot) TrySurpress(m *gateway.MessageCreateEvent) {
	err := b.storage.TryResetQuotaOnNextDay(uint64(m.Author.ID), uint64(m.ChannelID))
	if err != nil {
		log.Printf("Error resetting restore count: %v", err)
	}

	usage, err := b.storage.GetQuotaUsage(uint64(m.Author.ID), uint64(m.ChannelID))
	if errors.Is(err, sql.ErrNoRows) {
		err = b.storage.ResetQuotaUsage(uint64(m.Author.ID), uint64(m.ChannelID))
	}
	if err != nil {
		log.Printf("Error getting restore count: %v", err)
	}

	roleIDs := make([]uint64, len(m.Member.RoleIDs))
	for i, roleID := range m.Member.RoleIDs {
		roleIDs[i] = uint64(roleID)
	}
	quota, err := b.storage.GetQuotaByRoles(uint64(m.ChannelID), roleIDs)
	if errors.Is(err, sql.ErrNoRows) {
		quota = b.config.DefaultQuota
		err = nil
	}
	if err != nil {
		log.Printf("Error getting quota by roles: %v", err)
	}
	if quota == -1 {
		quota = b.config.DefaultQuota
	}

	if usage < quota {
		b.storage.IncreaseQuotaUsage(uint64(m.Author.ID), uint64(m.ChannelID))
	} else {
		if usage == quota {
			err = b.s.React(m.ChannelID, m.ID, discord.NewAPIEmoji(0, "🈚"))
			if err != nil {
				log.Printf("Error reacting to message: %v", err)
			}
		}
		b.Suppress(&m.Message)
	}
}

func (b *Bot) Suppress(m *discord.Message) {

	if m.Flags&discord.SuppressEmbeds != 0 {
		return
	}

	flags := m.Flags | discord.SuppressEmbeds
	// Suppress embeds for the message
	_, err := b.s.EditMessageComplex(m.ChannelID, m.ID, api.EditMessageData{
		Flags: &flags,
	})
	if err != nil {
		log.Printf("Error suppressing embeds: %v", err)
	}

	if m.Author.Bot {
		channelSuppressingBot, err := b.storage.IsChannelSuppressBot(uint64(m.ChannelID))
		if err != nil {
			log.Printf("Error checking channel status: %v", err)
			return
		}
		if !channelSuppressingBot {
			return
		}
	}

	user, err := b.storage.GetUser(uint64(m.Author.ID))
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		log.Printf("Error getting last hint at: %v", err)
	}

	if !m.Author.Bot && time.Now().After(user.NextHintAt) {
		ch, err := b.s.CreatePrivateChannel(m.Author.ID)
		if err != nil {
			log.Printf("Error creating private channel: %v", err)
		}

		factor := int(math.Pow(2, float64(min(5, user.Hinted))))
		var cooldown = 24 * factor

		_, err = b.s.SendMessage(ch.ID, fmt.Sprintf("<#%d>頻道已啟用嵌入限流，您方才發送的訊息已抑制嵌入。\n若有需要回收遷入額度請右鍵訊息 > APP 選單中選擇「抑制嵌入」\n-# - 每人每天有限量嵌入額度\n-# - %d 小時內不會再收到此提示", m.ChannelID, cooldown))
		if err != nil {
			log.Printf("Error sending message: %v", err)
		}

		err = b.storage.SetNextHintAt(uint64(m.Author.ID), time.Now().Add(time.Duration(cooldown)*time.Hour))
		if err != nil {
			log.Printf("Error setting next hint at: %v", err)
		}
	}
}

type InteractionTokenCache struct {
	CreatedAt time.Time
	IType     discord.InteractionDataType
	Id        string
}

var interactionTokenCache *otter.Cache[string, InteractionTokenCache]

func init() {
	c, err := otter.MustBuilder[string, InteractionTokenCache](128).WithTTL(time.Second * 5).DeletionListener(func(key string, value InteractionTokenCache, cause otter.DeletionCause) {
		switch cause {
		case otter.Expired:
			log.Printf("interaction %s expired after %s", key, time.Since(value.CreatedAt))
		}
	}).Build()
	if err != nil {
		panic(err)
	}
	interactionTokenCache = &c
}

func (b *Bot) handleInteractionCreate(e *gateway.InteractionCreateEvent) {
	var err error
	defer func() {
		interactionTokenCache.Delete(e.Token)
		if err != nil {
			log.Printf("ERR: %+v\n", err)
		}
		err := recover()
		if err != nil {
			log.Printf("PANIC: %+v\n%s", err, debug.Stack())
		}
	}()

	itCache := InteractionTokenCache{
		CreatedAt: time.Now(),
		IType:     e.Data.InteractionType(),
	}
	switch data := e.Data.(type) {
	case *discord.CommandInteraction:
		itCache.Id = data.Name
	case *discord.ButtonInteraction:
		itCache.Id = string(data.CustomID)
	case *discord.StringSelectInteraction:
		itCache.Id = string(data.CustomID)
	case *discord.ModalInteraction:
		itCache.Id = string(data.CustomID)
	}
	interactionTokenCache.Set(e.Token, itCache)

	state := InteractionHandlerState{}
	handler := func(e *gateway.InteractionCreateEvent, state *InteractionHandlerState, next ...Middleware[InteractionHandlerState]) error {
		var err error
		switch e.Data.InteractionType() {
		case discord.PingInteractionType:
		case discord.CommandInteractionType:
			switch e.Data.(*discord.CommandInteraction).Name {
			case "suppress_embeds":
				err = b.handleSuppressEmbeds(e)
			case "toggle_channel":
				err = b.handleToggleChannel(e)
			case "set_role_quota":
				err = b.handleSetRoleQuota(e)
			case "reset_quota":
				err = b.handleResetQuota(e)
			case "toggle_suppress_bot":
				err = b.handleToggleSuppressBot(e)
			case "list_role_quotas":
				err = b.handleListRoleQuotas(e)
			case "my_quota":
				err = b.handleMyQuota(e)
			}
		case discord.ComponentInteractionType:
		case discord.AutocompleteInteractionType:
		case discord.ModalInteractionType:
		}
		return err
	}
	err = PanicRecoveryMiddleware(e, &state, LoggingMiddleware, handler)

	if err != nil {
		log.Printf("Error handling interaction: %v", err)
	}
}

func (b *Bot) handleSuppressEmbeds(e *gateway.InteractionCreateEvent) error {
	sender := e.SenderID()
	channelId := e.ChannelID
	data := e.Data.(*discord.CommandInteraction)

	msg, ok := data.Resolved.Messages[data.TargetMessageID()]
	if !ok {
		return b.s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString("Message not found"),
				Flags:   discord.EphemeralMessage,
			},
		})
	}

	if msg.Author.ID != sender {
		if !(msg.Author.ID == 1290664871993806932 && strings.HasPrefix(msg.Content, fmt.Sprintf("<@%d>", sender))) {
			return b.s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
				Type: api.MessageInteractionWithSource,
				Data: &api.InteractionResponseData{
					Content: option.NewNullableString("-# ❌ 您不是此訊息的作者"),
					Flags:   discord.EphemeralMessage,
				},
			})
		}
	}
	if msg.Flags&discord.SuppressEmbeds > 0 {
		respd := api.InteractionResponseData{
			Content: option.NewNullableString("-# ❌ 此訊息已抑制嵌入"),
			Flags:   discord.EphemeralMessage,
		}
		return b.s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &respd,
		})
	}

	if time.Since(msg.Timestamp.Time()) > time.Minute {
		return b.s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString("-# ❌ 無法在一分鐘後回收額度"),
				Flags: discord.EphemeralMessage,
			},
		})
	}

	flags := msg.Flags | discord.SuppressEmbeds
	// Suppress embeds for the message
	_, err := b.s.EditMessageComplex(msg.ChannelID, msg.ID, api.EditMessageData{
		Flags: &flags,
	})

	remaining, err := b.storage.DecreaseQuotaUsage(uint64(sender), uint64(channelId))
	if err != nil {
		log.Printf("Error decrementing restore count: %v", err)
	}

	roleIDs := make([]uint64, len(e.Member.RoleIDs))
	for i, roleID := range e.Member.RoleIDs {
		roleIDs[i] = uint64(roleID)
	}
	quota, err := b.storage.GetQuotaByRoles(uint64(e.ChannelID), roleIDs)
	if errors.Is(err, sql.ErrNoRows) {
		quota = b.config.DefaultQuota
		err = nil
	}
	if err != nil {
		log.Printf("Error getting quota by roles: %v", err)
	}

	respd := api.InteractionResponseData{
		Content: option.NewNullableString(fmt.Sprintf("-# ✅ 於此頻道展開額度：%d/%d", quota-remaining, quota)),
		Flags:   discord.EphemeralMessage,
	}
	err = b.s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &respd,
	})
	if err != nil {
		log.Printf("Error responding to interaction: %v", err)
	}

	return err
}

func (b *Bot) handleToggleChannel(i *gateway.InteractionCreateEvent) error {
	// Check if user has manage channel permission
	perms, err := b.s.Permissions(i.ChannelID, i.Member.User.ID)
	if err != nil {
		return b.s.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString("Error checking permissions"),
				Flags:   discord.EphemeralMessage,
			},
		})
	}

	if !perms.Has(discord.PermissionManageChannels) {
		return b.s.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString("You need the Manage Channels permission to use this command"),
				Flags:   discord.EphemeralMessage,
			},
		})
	}

	enabled, err := b.storage.IsChannelEnabled(uint64(i.ChannelID))
	if err != nil {
		return b.s.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString("Error checking channel status"),
				Flags:   discord.EphemeralMessage,
			},
		})
	}

	err = b.storage.SetChannelEnabled(uint64(i.ChannelID), !enabled)
	if err != nil {
		return b.s.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString("Error toggling channel status"),
				Flags:   discord.EphemeralMessage,
			},
		})
	}

	status := "disabled"
	if !enabled {
		status = "enabled"
	}
	return b.s.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString(fmt.Sprintf("Embed throttling has been %s for this channel", status)),
			Flags:   discord.EphemeralMessage,
		},
	})
}

func (b *Bot) handleToggleSuppressBot(i *gateway.InteractionCreateEvent) error {
	channelSuppressingBot, err := b.storage.IsChannelSuppressBot(uint64(i.ChannelID))
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return nil
	}

	err = b.storage.SetChannelSuppressBot(uint64(i.ChannelID), !channelSuppressingBot)
	if err != nil {
		return err
	}

	var msg string
	if !channelSuppressingBot {
		msg = fmt.Sprintf("-# ✅ 此頻道已**啟用**抑制機器人訊息嵌入")
	} else {
		msg = fmt.Sprintf("-# ✅ 此頻道已**停用**抑制機器人訊息嵌入")
	}
	respd := api.InteractionResponseData{
		Content: option.NewNullableString(msg),
		Flags:   discord.EphemeralMessage,
	}
	return b.s.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &respd,
	})
}

func (b *Bot) handleSetRoleQuota(i *gateway.InteractionCreateEvent) error {
	data := i.Data.(*discord.CommandInteraction)
	roleID, err := data.Options.Find("role").SnowflakeValue()
	if err != nil {
		return err
	}

	quota, err := data.Options.Find("quota").IntValue()
	if err != nil {
		return err
	}

	priority, err := data.Options.Find("priority").IntValue()
	if err != nil {
		return err
	}

	err = b.storage.ConfigureRoleQuota(uint64(i.ChannelID), uint64(roleID), int(quota), int(priority))
	if err != nil {
		return err
	}

	respd := api.InteractionResponseData{
		Content: option.NewNullableString(fmt.Sprintf("-# ✅ 身分組 <@&%d> 的嵌入限流額度已設定為 %d", roleID, quota)),
		Flags:   discord.EphemeralMessage,
	}
	return b.s.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &respd,
	})
}

func (b *Bot) handleResetQuota(i *gateway.InteractionCreateEvent) error {
	data := i.Data.(*discord.CommandInteraction)
	userID, err := data.Options.Find("user").SnowflakeValue()
	if err != nil {
		return err
	}

	err = b.storage.ResetQuotaUsage(uint64(userID), uint64(i.ChannelID))
	if err != nil {
		return err
	}

	respd := api.InteractionResponseData{
		Content: option.NewNullableString(fmt.Sprintf("-# ✅ 已重設 <@%d> 的嵌入額度", userID)),
		Flags:   discord.EphemeralMessage,
	}
	return b.s.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &respd,
	})
}

func (b *Bot) handleListRoleQuotas(i *gateway.InteractionCreateEvent) error {
	quotas, err := b.storage.GetAllRoleQuotas(uint64(i.ChannelID))
	if err != nil {
		return err
	}

	sb := strings.Builder{}
	sb.WriteString("-# 以下為此頻道所有身分組嵌入限流設定：\n")
	for _, quota := range quotas {
		sb.WriteString(fmt.Sprintf("-# - <@&%d>：%d (p%d)\n", quota.RoleID, quota.Quota, quota.Priority))
	}

	respd := api.InteractionResponseData{
		Content: option.NewNullableString(sb.String()),
		Flags:   discord.EphemeralMessage,
	}
	return b.s.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &respd,
	})
}

func (b *Bot) handleMyQuota(e *gateway.InteractionCreateEvent) error {
	usage, err := b.storage.GetQuotaUsage(uint64(e.Member.User.ID), uint64(e.ChannelID))
	if errors.Is(err, sql.ErrNoRows) {
		err = b.storage.ResetQuotaUsage(uint64(e.Member.User.ID), uint64(e.ChannelID))
	}
	if err != nil {
		return err
	}

	roleIDs := make([]uint64, len(e.Member.RoleIDs))
	for i, roleID := range e.Member.RoleIDs {
		roleIDs[i] = uint64(roleID)
	}
	quota, err := b.storage.GetQuotaByRoles(uint64(e.ChannelID), roleIDs)
	if errors.Is(err, sql.ErrNoRows) {
		quota = b.config.DefaultQuota
		err = nil
	}
	if err != nil {
		log.Printf("Error getting quota by roles: %v", err)
	}

	respd := api.InteractionResponseData{
		Content: option.NewNullableString(fmt.Sprintf("-# ✅ 於此頻道展開額度：%d/%d", quota-usage, quota)),
		Flags:   discord.EphemeralMessage,
	}
	err = b.s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &respd,
	})
	return err
}
