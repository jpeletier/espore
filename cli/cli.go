package cli

import (
	"errors"
	"espore/builder"
	"espore/cli/syncer"
	"espore/session"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/gdamore/tcell"
	"gitlab.com/tslocum/cview"
)

type Config struct {
	Session     *session.Session
	OnQuit      func()
	BuildConfig *builder.BuildConfig
}

type CLI struct {
	Config
	dumper          *Dumper
	app             *cview.Application
	input           *cview.InputField
	textView        *cview.TextView
	commandHandlers map[string]*commandHandler
	syncers         map[string]*syncer.Syncer
}

var commandRegex = regexp.MustCompile(`(?m)^\/([^ ]*) *(.*)$`)
var errQuit = errors.New("User quit")

const MAX_TEXT_BUFFER = 10000

func New(config *Config) *CLI {

	cli := &CLI{
		Config:  *config,
		syncers: make(map[string]*syncer.Syncer),
	}
	cli.commandHandlers = cli.buildCommandHandlers()
	cli.Session.Log = cli

	return cli
}

func (c *CLI) parseCommandLine(cmdline string) error {
	match := commandRegex.FindStringSubmatch(cmdline)
	if len(match) > 0 {
		command := match[1]
		parameters := strings.Split(match[2], " ")
		handler := c.commandHandlers[command]
		if handler == nil {
			c.Printf("Unknown command %q\n", command)
			return nil
		}
		if len(parameters) < handler.minParameters {
			c.Printf("Expected at least %d parameters. Got %d\n", handler.minParameters, len(parameters))
			return nil
		}
		c.dumper.Stop()
		defer c.dumper.Dump()
		return handler.handler(parameters)
	}
	return c.Session.SendCommand(cmdline)
}

func (c *CLI) Printf(format string, a ...interface{}) {
	fmt.Fprintf(c.textView, format, a...)
	/*	c.app.QueueUpdateDraw(func() {
		fmt.Printf("Q: %s", format)
	})*/
}

func (c *CLI) Run() error {
	var history []string
	var historyPos int

	var appError error
	app := cview.NewApplication()
	flexbox := cview.NewFlex()
	input := cview.NewInputField()

	var textView *cview.TextView
	textView = cview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(true).
		SetMaxLines(300).
		SetScrollable(true).
		ScrollToEnd().
		SetChangedFunc(func() {
			app.Draw()
		})

	textView.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyTAB {
			app.SetFocus(input)
		}

	})
	textView.SetBorder(true)

	flexbox.SetDirection(cview.FlexRow)
	flexbox.AddItem(textView, 0, 1, false)
	flexbox.AddItem(input, 1, 0, true)

	commands := make(chan func(), 10)
	go func() {
		wg := sync.WaitGroup{}
		for cmdFunc := range commands {
			wg.Add(1)
			app.QueueUpdate(func() {
				go func() {
					defer wg.Done()
					cmdFunc()
				}()
			})
			wg.Wait()
		}

	}()

	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTAB:
			app.SetFocus(textView)
		case tcell.KeyEnter:
			cmd := strings.TrimSpace(input.GetText())
			if len(cmd) == 0 {
				return
			}
			input.SetText("")
			commands <- func() {
				err := c.parseCommandLine(cmd)
				if err != nil {
					c.Printf("Error executing command: %s", err)
				}
			}
			lh := len(history)
			if lh == 0 || (lh > 0 && history[lh-1] != cmd) {
				history = append(history, cmd)
				historyPos = lh + 1
			}
		}
	})

	input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			if historyPos > 0 {
				historyPos--
				input.SetText(history[historyPos])
			}
			return nil
		case tcell.KeyDown:
			if historyPos < len(history)-1 {
				historyPos++
				input.SetText(history[historyPos])
			} else {
				input.SetText("")
			}
			return nil

		}
		return event
	})

	c.dumper = &Dumper{
		R: c.Session,
		W: textView,
	}
	c.dumper.Dump()
	defer c.dumper.Stop()
	c.app = app
	c.input = input
	c.textView = textView

	if err := app.SetRoot(flexbox, true).Run(); err != nil {
		panic(err)
	}
	close(commands)

	return appError
}
