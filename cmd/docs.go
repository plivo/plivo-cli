package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/plivo/plivo-cli/internal/docs"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// `plivo docs` reads the docs site's own llms.txt / llms-full.txt exports, so it
// needs no credentials and no server-side search. Deliberately does not call
// getClient — asking someone to log in to read public docs would be absurd, and
// it means this works in a fresh container or CI job.

var (
	docsLimit   int
	docsRefresh bool

	docsFetcherForTest *docs.Fetcher
)

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Read the Plivo docs from the terminal",
	Long: `Read the Plivo documentation without leaving the shell.

Backed by the docs site's own machine-readable exports, so no credentials are
needed. The full text is cached under ~/.plivo/cache for a day; pass --refresh
to re-fetch.`,
}

var docsSearchCmd = &cobra.Command{
	Use:   "search <keywords...>",
	Short: "Search the docs full text",
	Long: `Search every documentation page.

A page matches only if it contains ALL the keywords, ranked by how often they
appear. Matching is case-insensitive and substring-based, so partial words and
API field names both work.`,
	Example: `  plivo docs search audio streaming
  plivo docs search bidirectional
  plivo docs search 10dlc brand registration -o json`,
	Args: cobra.MinimumNArgs(1),
	RunE: runDocsSearch,
}

var docsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List every documentation page",
	Args:  cobra.NoArgs,
	RunE:  runDocsList,
}

var docsShowCmd = &cobra.Command{
	Use:   "show <path-or-title>",
	Short: "Print one documentation page",
	Long: `Print a page's full text.

The reference can be a URL, a path fragment such as voice/api/call, or a page
title. The most specific match wins.`,
	Example: `  plivo docs show voice/api/call
  plivo docs show "Account API"`,
	Args: cobra.ExactArgs(1),
	RunE: runDocsShow,
}

func init() {
	docsSearchCmd.Flags().IntVar(&docsLimit, "limit", 10, "maximum results")
	docsCmd.PersistentFlags().BoolVar(&docsRefresh, "refresh", false, "bypass the cache and re-fetch")
	docsCmd.AddCommand(docsSearchCmd, docsListCmd, docsShowCmd)
	rootCmd.AddCommand(docsCmd)
}

func docsFetcher() *docs.Fetcher {
	if docsFetcherForTest != nil {
		return docsFetcherForTest
	}
	return docs.New()
}

func runDocsSearch(cmd *cobra.Command, args []string) error {
	matches, err := docsFetcher().Search(args, docsLimit, docsRefresh)
	if err != nil {
		return err
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, matches, map[string]any{"count": len(matches)})
	}
	if len(matches) == 0 {
		fmt.Fprintf(os.Stderr, "No docs matched %q.\n", strings.Join(args, " "))
		return nil
	}
	for _, m := range matches {
		fmt.Printf("%s\n  %s\n", m.Title, m.Source)
		if m.Snippet != "" {
			fmt.Printf("  %s\n", m.Snippet)
		}
		fmt.Println()
	}
	return nil
}

func runDocsList(cmd *cobra.Command, args []string) error {
	pages, err := docsFetcher().Index(docsRefresh)
	if err != nil {
		return err
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, pages, map[string]any{"count": len(pages)})
	}
	rows := [][]string{{"TITLE", "URL"}}
	for _, p := range pages {
		rows = append(rows, []string{p.Title, p.Source})
	}
	return output.Table(os.Stdout, rows)
}

func runDocsShow(cmd *cobra.Command, args []string) error {
	page, err := docsFetcher().Get(args[0], docsRefresh)
	if err != nil {
		return err
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, page, nil)
	}
	fmt.Printf("# %s\n%s\n\n%s\n", page.Title, page.Source, page.Body)
	return nil
}
