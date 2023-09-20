package log

import (
	"log/slog"
	"os"

	"gitlab.com/slxh/go/powerline"
)

// Level contains the application log level.
var Level slog.LevelVar

var colors = map[slog.Level]powerline.ColorScheme{
	slog.LevelDebug: {
		Time:    powerline.NewColor(99, powerline.ColorBlack),
		Level:   powerline.NewColor(powerline.ColorBlack, 99),
		Message: powerline.NewColor(99, powerline.ColorDefault),
	},
	slog.LevelInfo: {
		Time:    powerline.NewColor(45, powerline.ColorBlack),
		Level:   powerline.NewColor(powerline.ColorBlack, 45),
		Message: powerline.NewColor(45, powerline.ColorDefault),
	},
	slog.LevelWarn: {
		Time:    powerline.NewColor(220, powerline.ColorBlack),
		Level:   powerline.NewColor(powerline.ColorBlack, 220),
		Message: powerline.NewColor(220, powerline.ColorDefault),
	},
	slog.LevelError: {
		Time:    powerline.NewColor(208, powerline.ColorBlack),
		Level:   powerline.NewColor(powerline.ColorBlack, 208),
		Message: powerline.NewColor(208, powerline.ColorDefault),
	},
}

func setLogger(h slog.Handler) {
	slog.SetDefault(slog.New(h))
}

func onTTY() bool {
	s, _ := os.Stdout.Stat()

	return s.Mode()&os.ModeCharDevice > 0
}

func SetColoredLogger() {
	setLogger(powerline.NewHandler(os.Stdout, &powerline.HandlerOptions{
		Level:  &Level,
		Colors: colors,
	}))
}

func SetUncoloredLogger() {
	setLogger(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: &Level,
	}))
}

func SetLogger() {
	if onTTY() {
		SetColoredLogger()
	} else {
		SetUncoloredLogger()
	}
}

func Panic(msg string, args ...any) {
	slog.Error(msg, args...)
	panic(msg)
}
