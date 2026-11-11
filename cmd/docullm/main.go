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
	"docullm/internal/docparse/docbook"
	"docullm/internal/docparse/loader"
	"docullm/internal/docparse/markdown"
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

type StringSlice []string

func (s *StringSlice) String() string {
	return fmt.Sprint(*s)
}

func (s *StringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	var directories StringSlice
	var files StringSlice
	flag.Var(&directories, "d", "directory to read all DocBook DC, markdown and text files from, may be given more than once")
	flag.Var(&files, "f", "doc file to parse (DC file, markdown or text), may be given more than once or as arguments (default: stdin)")
	tree := flag.Bool("tree", false, "dump the raw markdown node tree as json")
	asJSON := flag.Bool("json", false, "print documents as json instead of yaml")
	serve := flag.Bool("mcp", false, "serve the documents as MCP server on stdin/stdout")
	web := flag.Bool("web", false, "serve the documents as a small web interface")
	port := flag.Int("port", 8080, "port the web interface listens on (with -web)")
	debugFlag := flag.Bool("debug", false, "log tool calls, web requests and DocBook parsing problems to stderr")
	flag.Parse()
	debug = *debugFlag
	mcpserver.Debug = debug
	docbook.Debug = debug
	files = append(files, flag.Args()...)

	if *tree {
		var data []byte
		var err error
		if len(files) > 0 {
			data, err = os.ReadFile(files[0])
		} else {
			data, err = io.ReadAll(os.Stdin)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "error reading input:", err)
			os.Exit(1)
		}
		enc, err := json.MarshalIndent(markdown.BuildTree(data), "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error encoding json:", err)
			os.Exit(1)
		}
		fmt.Println(string(enc))
		return
	}

	if len(directories) == 0 && len(files) == 0 {
		if *serve || *web {
			fmt.Fprintln(os.Stderr, "-mcp and -web need a file (-f) or a directory (-d)")
			os.Exit(1)
		}
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error reading input:", err)
			os.Exit(1)
		}
		printCorpus(markdown.BuildDocuments(markdown.SplitSources(data, "stdin")), *asJSON)
		return
	}

	var docs []loader.DocFile
	for _, dir := range directories {
		found, err := loader.Find(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error reading input:", err)
			os.Exit(1)
		}
		docs = append(docs, found...)
	}
	for _, f := range files {
		docs = append(docs, loader.File(f))
	}
	corpus, err := loader.Load(docs)
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
}
