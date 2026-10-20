package engine

import (
	"fmt"
	"sync"
)

var consoleTitleMu sync.Mutex

func setConsoleTitle(title string) {
	if title == "" {
		return
	}
	consoleTitleMu.Lock()
	defer consoleTitleMu.Unlock()
	fmt.Printf("\033]0;%s\007", title)
}

func SetConsoleTitle(title string) {
	setConsoleTitle(title)
}
