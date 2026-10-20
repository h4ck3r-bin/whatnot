package logger

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	reset  = "\033[0m"
	dim    = "\033[2m"
	red    = "\033[31m"
	blue   = "\033[34m"
	green  = "\033[32m"
	cyan   = "\033[36m"
	yellow = "\033[33m"
	pink   = "\033[95m"
	hiRed  = "\033[91m"
)

var (
	threadCounter atomic.Int64
	globalMu      sync.Mutex
)

func NextID() string {
	return fmt.Sprintf("W%02d", threadCounter.Add(1))
}

type Logger struct {
	id string
	mu sync.Mutex
}

func New(id string) *Logger {
	return &Logger{id: id}
}

func (l *Logger) log(color, component, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Printf("%s[%s] %s%s\n", color, component, msg, reset)
}

func (l *Logger) Manager(msg string)       { l.log(dim, "MANAGER", msg) }
func (l *Logger) ManagerNotice(msg string) { l.log(cyan, "MANAGER", msg) }
func (l *Logger) ManagerError(msg string)  { l.log(red, "MANAGER", msg) }
func (l *Logger) Whatnot(msg string)       { l.log(blue, "WHATNOT", msg) }
func (l *Logger) Refresh(msg string)       { l.log(cyan, "SESSION", msg) }
func (l *Logger) RefreshError(msg string)  { l.log(red, "REFRESH", msg) }
func (l *Logger) Kasada(msg string)        { l.log(pink, "KASADA", msg) }
func (l *Logger) Seon(msg string)          { l.log(yellow, "SEON", msg) }
func (l *Logger) Phone(msg string)         { l.log(pink, "PHONE", msg) }
func (l *Logger) PhoneError(msg string)    { l.log(red, "PHONE", msg) }
func (l *Logger) HeroSMS(msg string)       { l.log(yellow, "HEROSMS", msg) }
func (l *Logger) Scope(msg string)         { l.log(cyan, "SCOPE", msg) }
func (l *Logger) Checkout(msg string)      { l.log(green, "CHECKOUT", msg) }
func (l *Logger) Review(msg string)        { l.log(green, "REVIEW", msg) }
func (l *Logger) Order(msg string)         { l.log(cyan, "ORDER", msg) }
func (l *Logger) Events(msg string)        { l.log(cyan, "EVENTS", msg) }
func (l *Logger) Error(msg string)         { l.log(red, "ERR", msg) }

func printGlobal(color, component, msg string) {
	globalMu.Lock()
	defer globalMu.Unlock()
	fmt.Printf("%s[%s] %s%s\n", color, component, msg, reset)
}

func Manager(msg string) { printGlobal(dim, "MANAGER", msg) }
func ManagerNotice(msg string) {
	printGlobal(cyan, "MANAGER", msg)
}
func ManagerError(msg string) {
	printGlobal(red, "MANAGER", msg)
}
func Whatnot(msg string) { printGlobal(green, "WHATNOT", msg) }
func Error(msg string)   { printGlobal(red, "ERR", msg) }
