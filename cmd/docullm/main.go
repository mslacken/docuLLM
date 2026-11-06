package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"docullm/internal/docparse"
	"docullm/internal/mcpserver"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

func printCorpus(c *docparse.Corpus, asJSON bool) {
	var enc []byte
	var err error
	if asJSON {
		enc, err = json.MarshalIndent(c, "", "  ")
	} else {
		enc, err = yaml.Marshal(c)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error encoding output:", err)
		os.Exit(1)
	}
	fmt.Print(string(enc))
}

var debug bool

func main() {
	file := flag.String("f", "", "markdown file to parse, may also be given as argument (default: stdin)")
	tree := flag.Bool("tree", false, "dump the raw markdown node tree as json")
	asJSON := flag.Bool("json", false, "print documents as json instead of yaml")
	dir := flag.String("d", "", "directory of markdown files (or of DocBook DC files, or a single DC file) to read instead of a single file")
	serve := flag.Bool("mcp", false, "serve the documents as MCP server on stdin/stdout")
	web := flag.Bool("web", false, "serve the documents as a small web interface")
	port := flag.Int("port", 8080, "port the web interface listens on (with -web)")
	debugFlag := flag.Bool("debug", false, "log tool calls, web requests and DocBook parsing problems to stderr")
	flag.Parse()
	debug = *debugFlag
	mcpserver.Debug = debug
	docparse.Debug = debug
	if *file == "" && flag.NArg() > 0 {
		*file = flag.Arg(0)
	}

	if *serve || *dir != "" || *web {
		root := *dir
		if root == "" {
			root = *file
		}
		if root == "" {
			fmt.Fprintln(os.Stderr, "-mcp and -web need a file or a directory (-d)")
			os.Exit(1)
		}
		corpus, err := docparse.LoadCorpus(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error reading input:", err)
			os.Exit(1)
		}
		if *web {
			addr := fmt.Sprintf(":%d", *port)
			fmt.Fprintf(os.Stderr, "serving web interface on http://localhost%s/\n", addr)
			if err := http.ListenAndServe(addr, serveWeb(corpus)); err != nil {
				fmt.Fprintln(os.Stderr, "web server:", err)
				os.Exit(1)
			}
			return
		}
		if *serve {
			if err := mcpserver.NewServer(corpus).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
				fmt.Fprintln(os.Stderr, "mcp server:", err)
				os.Exit(1)
			}
			return
		}
		printCorpus(corpus, *asJSON)
		return
	}

	var data []byte
	var err error
	if *file != "" {
		data, err = os.ReadFile(*file)
	} else {
		data, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error reading input:", err)
		os.Exit(1)
	}

	if !*tree {
		name := *file
		if name == "" {
			name = "stdin"
		}
		printCorpus(docparse.BuildDocuments(docparse.SplitSources(data, name)), *asJSON)
		return
	}

	out := docparse.BuildTree(data)

	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error encoding json:", err)
		os.Exit(1)
	}
	fmt.Println(string(enc))
}
