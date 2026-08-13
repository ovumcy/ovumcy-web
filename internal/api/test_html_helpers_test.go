package api

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func mustParseHTMLDocument(t *testing.T, markup string) *html.Node {
	t.Helper()

	root, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatalf("parse html document: %v", err)
	}
	return root
}

func htmlDocumentText(root *html.Node) string {
	return normalizeHTMLText(htmlNodeText(root))
}

func htmlNodeText(root *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}
		if node.Type == html.TextNode {
			builder.WriteString(node.Data)
			builder.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return builder.String()
}

func normalizeHTMLText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func htmlElementByID(root *html.Node, id string) *html.Node {
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "id") == id
	})
}

func htmlElementByTagAndClass(root *html.Node, tag string, className string) *html.Node {
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == tag && htmlHasClass(node, className)
	})
}

func htmlElementByAttr(root *html.Node, name string, value string) *html.Node {
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, name) == value
	})
}

// htmlSectionIDs lists the ids of the page's identified sections in document
// order. A section is recognised by what it IS, not by the tag it happens to
// be spelled with: the settings cards are <details> disclosures, and a guard
// keyed on the literal "section" reported the settings page as having no
// sections at all the day they were converted. Anything carrying the
// data-settings-section marker counts, whatever its tag.
func htmlSectionIDs(root *html.Node) []string {
	sections := htmlFindElements(root, func(node *html.Node) bool {
		if node.Type != html.ElementNode || htmlAttr(node, "id") == "" {
			return false
		}
		return node.Data == "section" || htmlHasAttr(node, "data-settings-section")
	})

	ids := make([]string, 0, len(sections))
	for _, section := range sections {
		ids = append(ids, htmlAttr(section, "id"))
	}
	return ids
}

// htmlRadioValues lists the value attributes of one radio group in document
// order, so a test can pin the order options are offered in.
func htmlRadioValues(root *html.Node, name string) []string {
	radios := htmlFindElements(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "input" &&
			htmlAttr(node, "type") == "radio" && htmlAttr(node, "name") == name
	})

	values := make([]string, 0, len(radios))
	for _, radio := range radios {
		values = append(values, htmlAttr(radio, "value"))
	}
	return values
}

func htmlFindElement(root *html.Node, predicate func(*html.Node) bool) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || found != nil {
			return
		}
		if predicate(node) {
			found = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func htmlFindElements(root *html.Node, predicate func(*html.Node) bool) []*html.Node {
	var elements []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}
		if predicate(node) {
			elements = append(elements, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return elements
}

func htmlAttr(node *html.Node, name string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func htmlHasAttr(node *html.Node, name string) bool {
	if node == nil {
		return false
	}
	for _, attr := range node.Attr {
		if attr.Key == name {
			return true
		}
	}
	return false
}

func htmlHasClass(node *html.Node, className string) bool {
	classes := strings.Fields(htmlAttr(node, "class"))
	for _, class := range classes {
		if class == className {
			return true
		}
	}
	return false
}

func htmlFlashByKey(root *html.Node, key string) *html.Node {
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-flash-key") == key
	})
}

func htmlAuthErrorByKey(root *html.Node, key string) *html.Node {
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-error-key") == key
	})
}
