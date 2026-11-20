// Command drizzle-go 从结构体 tag 生成表描述符和扫描函数。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/xbmlz/drizzle-go/codegen"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "generate" {
		fmt.Fprintln(os.Stderr, "usage: drizzle-go generate [-dir .]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	dir := fs.String("dir", ".", "schema package directory")
	_ = fs.Parse(os.Args[2:])
	if err := codegen.Generate(*dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
