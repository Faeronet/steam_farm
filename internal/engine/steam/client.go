package steam

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/paralin/go-steam"
	"github.com/paralin/go-steam/protocol/steamlang"
	"github.com/paralin/go-steam/steamid"

	"github.com/faeronet/steam-farm-system/internal/engine/guard"
)

type ClientState int

const (
	StateDisconnected ClientState = iota
	StateConnecting
	StateConnected
	StateLoggingIn
	StateLoggedIn
	StatePlaying
)

func (s ClientState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateLoggingIn:
		return "logging_in"
	case StateLoggedIn:
		return "logged_in"
	case StatePlaying:
		return "playing"
	default:
		return "unknown"
	}
}

type EventHandler func(event interface{})

type Client struct {
	mu sync.RWMutex

	state        ClientState
	username     string
	password     string
	sharedSecret string
	steamID      steamid.SteamId

	onEvent       EventHandler
	maxReconnects int

	playAppID uint64
}

type ClientConfig struct {
	Username     string
	Password     string
	SharedSecret string
	AppID        uint64
	OnEvent      EventHandler
}

func NewClient(cfg ClientConfig) *Client {
	return &Client{
		username:      cfg.Username,
		password:      cfg.Password,
		sharedSecret:  cfg.SharedSecret,
		playAppID:     cfg.AppID,
		onEvent:       cfg.OnEvent,
		state:         StateDisconnected,
		maxReconnects: 50,
	}
}

func (c *Client) setState(s ClientState) {
	c.mu.Lock()
	c.state = s
	c.mu.Unlock()
}

func (c *Client) State() ClientState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *Client) SteamID() steamid.SteamId {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.steamID
}

func (c *Client) Run(ctx context.Context) error {
	reconnects := 0
	wasPlaying := false

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		sc := steam.NewClient()
		c.setState(StateConnecting)

		log.Printf("[%s] Connecting to Steam CM servers...", c.username)

		connectDone := make(chan struct{})
		go func() {
			sc.Connect()
			close(connectDone)
		}()

		select {
		case <-ctx.Done():
			sc.Disconnect()
			return ctx.Err()
		case <-connectDone:
		case <-time.After(30 * time.Second):
			log.Printf("[%s] Connect timed out (30s), retrying...", c.username)
			sc.Disconnect()
			reconnects++
			if reconnects > c.maxReconnects {
				return fmt.Errorf("max reconnects (%d) exhausted", c.maxReconnects)
			}
			c.sleepWithContext(ctx, reconnectDelay(reconnects))
			continue
		}

		log.Printf("[%s] Processing events...", c.username)

		reason, playDuration := c.eventLoop(ctx, sc, wasPlaying)

		sc.Disconnect()

		if ctx.Err() != nil {
			return ctx.Err()
		}

		wasPlaying = reason == loopLostWhilePlaying || reason == loopLostWhileLoggedIn

		if playDuration > 60*time.Second {
			reconnects = 0
			log.Printf("[%s] Session lasted %v, reconnect counter reset", c.username, playDuration.Round(time.Second))
		}

		switch reason {
		case loopLoginFailed:
			return fmt.Errorf("login failed")
		case loopMaxReconnects:
			return fmt.Errorf("max reconnects exhausted")
		case loopChannelClosed, loopFatalError, loopDisconnected,
			loopLostWhilePlaying, loopLostWhileLoggedIn:
			reconnects++
			if reconnects > c.maxReconnects {
				log.Printf("[%s] Max reconnects (%d) reached", c.username, c.maxReconnects)
				if c.onEvent != nil {
					c.onEvent(&LoginFailedEvent{Username: c.username, Reason: "max reconnects reached"})
				}
				return fmt.Errorf("max reconnects exhausted")
			}
			delay := reconnectDelay(reconnects)
			log.Printf("[%s] Reconnecting in %v (attempt %d/%d)...", c.username, delay, reconnects, c.maxReconnects)
			if wasPlaying && c.onEvent != nil {
				c.onEvent(&ConnectionLostEvent{Username: c.username, Reason: "connection lost, reconnecting"})
			}
			c.sleepWithContext(ctx, delay)
		}
	}
}

type loopExitReason int

const (
	loopChannelClosed loopExitReason = iota
	loopFatalError
	loopDisconnected
	loopLoginFailed
	loopLostWhilePlaying
	loopLostWhileLoggedIn
	loopMaxReconnects
	loopContextDone
)

func (c *Client) eventLoop(ctx context.Context, sc *steam.Client, resumeGame bool) (loopExitReason, time.Duration) {
	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()

	var playStart time.Time

	for {
		select {
		case <-ctx.Done():
			return loopContextDone, time.Since(playStart)

		case <-keepAlive.C:
			st := c.State()
			if st == StatePlaying && c.playAppID > 0 {
				sc.GC.SetGamesPlayed(c.playAppID)
			} else if st >= StateLoggedIn {
				sc.Social.SetPersonaState(steamlang.EPersonaState_Online)
			}

		case event, ok := <-sc.Events():
			if !ok {
				st := c.State()
				log.Printf("[%s] Events channel closed (state=%s)", c.username, st)
				c.setState(StateDisconnected)
				if st == StatePlaying {
					return loopLostWhilePlaying, time.Since(playStart)
				}
				if st == StateLoggedIn {
					return loopLostWhileLoggedIn, time.Since(playStart)
				}
				return loopChannelClosed, 0
			}

			switch e := event.(type) {
			case *steam.ConnectedEvent:
				c.setState(StateLoggingIn)
				log.Printf("[%s] TCP connected, authenticating...", c.username)

				var twoFactorCode string
				if c.sharedSecret != "" {
					code, err := guard.GenerateSteamTOTP(c.sharedSecret)
					if err != nil {
						log.Printf("[%s] TOTP failed: %v", c.username, err)
					} else {
						twoFactorCode = code
					}
				}

				sc.Auth.LogOn(&steam.LogOnDetails{
					Username:      c.username,
					Password:      c.password,
					TwoFactorCode: twoFactorCode,
				})

			case *steam.LoggedOnEvent:
				c.mu.Lock()
				c.state = StateLoggedIn
				c.steamID = sc.SteamId()
				c.mu.Unlock()

				log.Printf("[%s] Logged in! SteamID: %d", c.username, c.steamID)
				sc.Social.SetPersonaState(steamlang.EPersonaState_Online)

				if c.playAppID > 0 {
					c.setState(StatePlaying)
					sc.GC.SetGamesPlayed(c.playAppID)
					playStart = time.Now()
					log.Printf("[%s] Now playing AppID %d", c.username, c.playAppID)
				}

				if resumeGame {
					resumeGame = false
					log.Printf("[%s] Session resumed after reconnect", c.username)
					if c.onEvent != nil {
						c.onEvent(&ReconnectedEvent{SteamID: c.steamID, Username: c.username})
					}
				} else {
					if c.onEvent != nil {
						c.onEvent(&LoggedInEvent{SteamID: c.steamID, Username: c.username})
					}
				}

			case *steam.LogOnFailedEvent:
				c.setState(StateDisconnected)
				log.Printf("[%s] Login failed: %v", c.username, e.Result)
				if c.onEvent != nil {
					c.onEvent(&LoginFailedEvent{
						Username: c.username,
						Reason:   fmt.Sprintf("EResult %v", e.Result),
					})
				}
				return loopLoginFailed, 0

			case *steam.DisconnectedEvent:
				st := c.State()
				c.setState(StateDisconnected)
				dur := time.Since(playStart)
				log.Printf("[%s] Disconnected (was %s, session %v)", c.username, st, dur.Round(time.Second))
				if st == StatePlaying {
					return loopLostWhilePlaying, dur
				}
				if st == StateLoggedIn {
					return loopLostWhileLoggedIn, dur
				}
				return loopDisconnected, 0

			case steam.FatalErrorEvent:
				st := c.State()
				c.setState(StateDisconnected)
				dur := time.Since(playStart)
				log.Printf("[%s] Fatal: %v (was %s, session %v)", c.username, e, st, dur.Round(time.Second))
				if st == StatePlaying {
					return loopLostWhilePlaying, dur
				}
				if st == StateLoggedIn {
					return loopLostWhileLoggedIn, dur
				}
				return loopFatalError, 0

			default:
				if c.onEvent != nil {
					c.onEvent(event)
				}
			}
		}
	}
}

func (c *Client) sleepWithContext(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func reconnectDelay(attempt int) time.Duration {
	d := time.Duration(attempt*5) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

type LoggedInEvent struct {
	SteamID  steamid.SteamId
	Username string
}

type ReconnectedEvent struct {
	SteamID  steamid.SteamId
	Username string
}

type ConnectionLostEvent struct {
	Username string
	Reason   string
}

type LoginFailedEvent struct {
	Username string
	Reason   string
}
