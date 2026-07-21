package blog

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var markdownTableDelimiterCell = regexp.MustCompile(`^:?-{3,}:?$`)

// NormalizeLegacyMarkdownTables converts GFM table rows that an older rich
// editor stored as adjacent paragraphs. It keeps existing articles readable
// without requiring authors to open and republish every affected post.
func NormalizeLegacyMarkdownTables(raw string) string {
	if !strings.Contains(raw, "|") {
		return raw
	}

	root := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(raw), root)
	if err != nil {
		return raw
	}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	normalizeMarkdownTableChildren(root)

	var rendered strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		if err := html.Render(&rendered, node); err != nil {
			return raw
		}
	}
	return rendered.String()
}

func normalizeMarkdownTableChildren(parent *html.Node) {
	for node := parent.FirstChild; node != nil; {
		next := node.NextSibling
		header, ok := markdownTableRow(node)
		if !ok {
			normalizeMarkdownTableChildren(node)
			node = next
			continue
		}

		delimiterNode := nextMeaningfulSibling(node.NextSibling)
		delimiters, delimiterOK := markdownTableRow(delimiterNode)
		if !delimiterOK || len(header) != len(delimiters) || !validMarkdownTableDelimiter(delimiters) {
			normalizeMarkdownTableChildren(node)
			node = next
			continue
		}

		rows := make([][]string, 0)
		end := delimiterNode.NextSibling
		for {
			candidate := nextMeaningfulSibling(end)
			cells, rowOK := markdownTableRow(candidate)
			if !rowOK || len(cells) != len(header) {
				end = candidate
				break
			}
			rows = append(rows, cells)
			end = candidate.NextSibling
		}

		table := buildMarkdownTable(header, delimiters, rows)
		parent.InsertBefore(table, node)
		for current := node; current != end; {
			removeNext := current.NextSibling
			parent.RemoveChild(current)
			current = removeNext
			if current == nil {
				break
			}
		}
		node = end
	}
}

func nextMeaningfulSibling(node *html.Node) *html.Node {
	for current := node; current != nil; current = current.NextSibling {
		if current.Type == html.TextNode && strings.TrimSpace(current.Data) == "" {
			continue
		}
		if current.Type == html.ElementNode && current.Data == "p" && strings.TrimSpace(nodeText(current)) == "" {
			continue
		}
		return current
	}
	return nil
}

func markdownTableRow(node *html.Node) ([]string, bool) {
	if node == nil || node.Type != html.ElementNode || node.Data != "p" {
		return nil, false
	}
	line := strings.TrimSpace(nodeText(node))
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|"), "|")
	if len(parts) < 2 {
		return nil, false
	}
	for index := range parts {
		parts[index] = strings.TrimSpace(strings.ReplaceAll(parts[index], `\|`, "|"))
	}
	return parts, true
}

func validMarkdownTableDelimiter(cells []string) bool {
	for _, cell := range cells {
		if !markdownTableDelimiterCell.MatchString(cell) {
			return false
		}
	}
	return true
}

func buildMarkdownTable(header, delimiters []string, rows [][]string) *html.Node {
	table := elementNode("table")
	thead := elementNode("thead")
	tbody := elementNode("tbody")
	table.AppendChild(thead)
	table.AppendChild(tbody)

	headerRow := elementNode("tr")
	thead.AppendChild(headerRow)
	for index, value := range header {
		cell := elementNode("th")
		applyTableAlignment(cell, delimiters[index])
		cell.AppendChild(&html.Node{Type: html.TextNode, Data: value})
		headerRow.AppendChild(cell)
	}

	for _, values := range rows {
		row := elementNode("tr")
		for index, value := range values {
			cell := elementNode("td")
			applyTableAlignment(cell, delimiters[index])
			cell.AppendChild(&html.Node{Type: html.TextNode, Data: value})
			row.AppendChild(cell)
		}
		tbody.AppendChild(row)
	}
	return table
}

func applyTableAlignment(node *html.Node, delimiter string) {
	left := strings.HasPrefix(delimiter, ":")
	right := strings.HasSuffix(delimiter, ":")
	align := ""
	switch {
	case left && right:
		align = "center"
	case right:
		align = "right"
	case left:
		align = "left"
	}
	if align != "" {
		node.Attr = append(node.Attr, html.Attribute{Key: "style", Val: "text-align:" + align})
	}
}

func elementNode(name string) *html.Node {
	return &html.Node{Type: html.ElementNode, Data: name, DataAtom: atom.Lookup([]byte(name))}
}

func nodeText(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	var text strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(nodeText(child))
	}
	return text.String()
}
