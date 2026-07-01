package banner

import "fmt"

const Version = "0.3.0"

func PrintBanner() {
	fmt.Println(`
  _   _ _____  _       _____          _         _
 | | | |  ___| |     |  ___|        | |       | |
 | | | | |__ | |     | |__ _ __   __| |_ __ __| |
 | | | |  __|| |     |  __| '_ \ / _` + "`" + ` | '__/ _` + "`" + ` |
 | |_| | |___| |____ | |__| | | | (_| | | | (_| |
  \___/\____/\_____/\____/_| |_|\__,_|_|  \__,_|
`)
}

func PrintVersion() {
	fmt.Printf("Version: %s\n", Version)
}
