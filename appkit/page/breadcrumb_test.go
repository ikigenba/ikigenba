package page_test

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
)

func renderBreadcrumb(t *testing.T, data page.Banner) string {
	t.Helper()
	var output bytes.Buffer
	if err := page.Templates().ExecuteTemplate(&output, "banner", data); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestBreadcrumbEscaping(t *testing.T) {
	// R-FCTS-Z475 R-FE1P-CVXU R-KEI6-W1CP
	for _, test := range []struct {
		url, escaped string
	}{
		{"https://consumer.example.test/path?left=one&right=two", "https://consumer.example.test/path?left=one&amp;right=two"},
		{"https://consumer.example.test/path?value=<>\"'", "https://consumer.example.test/path?value=%3c%3e%22%27"},
		{"javascript:consumer(1)", "#ZgotmplZ"},
	} {
		for _, field := range []string{"Home", "LevelURL"} {
			t.Run(field+"/"+test.url, func(t *testing.T) {
				data := page.Banner{Trail: []page.Level{
					{Name: "parent<>&\"'", URL: "/parent-consumer"},
					{Name: "current<>&\"'", URL: "/current-consumer"},
				}}
				if field == "Home" {
					data.Home = test.url
				} else {
					data.Trail[0].URL = test.url
				}
				output := renderBreadcrumb(t, data)
				if !strings.Contains(output, test.escaped) || strings.Contains(output, test.url) {
					t.Fatalf("%s must emit escaped supplied URL %q", field, test.escaped)
				}
				for _, level := range data.Trail {
					if !strings.Contains(output, template.HTMLEscapeString(level.Name)) || strings.Contains(output, level.Name) {
						t.Fatalf("must emit escaped supplied name %q", level.Name)
					}
				}
			})
		}
	}
}

func TestBreadcrumbValueOrder(t *testing.T) {
	// R-KEI6-W1CP R-KFQ3-9T3E R-KGXZ-NKU3
	for _, count := range []int{1, 2, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			data := page.Banner{Home: "/consumer-home"}
			baseline := renderBreadcrumb(t, data)
			for i := range count {
				data.Trail = append(data.Trail, page.Level{
					Name: fmt.Sprintf("consumer_level_%d", i),
					URL:  fmt.Sprintf("/consumer_destination_%d", i),
				})
			}
			var values []string
			for i, level := range data.Trail {
				values = append(values, level.Name)
				if i < len(data.Trail)-1 {
					values = append(values, level.URL)
				}
			}
			for i, value := range values {
				if strings.Contains(baseline, value) {
					t.Fatalf("fixture value %q already present without Trail", value)
				}
				for j, other := range values {
					if i != j && strings.Contains(other, value) {
						t.Fatalf("fixture value %q occurs within %q", value, other)
					}
				}
			}
			output := renderBreadcrumb(t, data)
			previousName := -1
			for i, level := range data.Trail {
				name := strings.Index(output, level.Name)
				if name <= previousName {
					t.Fatalf("name %q absent or out of order", level.Name)
				}
				if i < len(data.Trail)-1 {
					url := strings.Index(output, level.URL)
					if url <= previousName || url >= name {
						t.Fatalf("URL %q absent or out of order", level.URL)
					}
				}
				previousName = name
			}
		})
	}
}

func TestBreadcrumbLastURLAbsent(t *testing.T) {
	// R-NNXD-MOSC
	for _, count := range []int{1, 2, 4} {
		data := page.Banner{}
		for i := range count {
			data.Trail = append(data.Trail, page.Level{Name: fmt.Sprintf("level_%d", i), URL: fmt.Sprintf("/destination_%d", i)})
		}
		last := &data.Trail[len(data.Trail)-1]
		last.URL = "/consumer_last_destination"
		original := last.URL
		last.URL = "/consumer_replacement_destination"
		if strings.Contains(renderBreadcrumb(t, data), original) {
			t.Fatal("fixture last URL occurs even after replacement")
		}
		last.URL = original
		if strings.Contains(renderBreadcrumb(t, data), original) {
			t.Fatal("last URL must not be emitted")
		}
	}
}

func TestBreadcrumbMenuRoutes(t *testing.T) {
	// R-JILO-5S1T R-FLD3-NIE0 R-JJTK-JJSI
	for _, trail := range [][]page.Level{
		nil, {}, {{Name: "current", URL: "/about"}},
		{{Name: "current", URL: "/tools"}},
		{{Name: "current", URL: "/consumer-other"}},
		{{Name: "parent", URL: "/consumer-parent"}, {Name: "current", URL: "/consumer-child"}},
	} {
		for _, tools := range []bool{false, true} {
			for _, home := range []string{"", "/consumer-home"} {
				data := page.Banner{Tools: tools, Home: home, Trail: trail}
				output := renderBreadcrumb(t, data)
				if !strings.Contains(output, "/about") {
					t.Fatalf("about route absent for %+v", data)
				}
				containsToolsValue := false
				for _, level := range trail {
					containsToolsValue = containsToolsValue || strings.Contains(level.Name, "/tools") || strings.Contains(level.URL, "/tools")
				}
				if (tools || !containsToolsValue) && strings.Contains(output, "/tools") != tools {
					t.Fatalf("tools route presence differs from Tools for %+v", data)
				}
			}
		}
	}
}

func TestBreadcrumbSingleLevelMarks(t *testing.T) {
	// R-FNSW-F1VE R-FP0S-STM3 R-FQ8P-6LCS R-FRGL-KD3H R-FSOH-Y4U6
	for _, tools := range []bool{false, true} {
		data := page.Banner{Tools: tools, Trail: []page.Level{{Name: "consumer-current"}}}
		renderAt := func(url string) string {
			data.Trail[0].URL = url
			return renderBreadcrumb(t, data)
		}
		other := renderAt("/consumer-other")
		about := renderAt("/about")
		toolsPage := renderAt("/tools")
		if about == other {
			t.Fatal("about page must differ from an unrelated page")
		}
		if tools {
			if toolsPage == other || toolsPage == about {
				t.Fatal("tools page must differ from unrelated and about pages")
			}
		} else if toolsPage != other {
			t.Fatal("unoffered tools page must equal unrelated page")
		}
		for _, url := range []string{"/consumer-another", "", "https://consumer.example.test/current", "javascript:consumer(1)"} {
			if renderAt(url) != other {
				t.Fatalf("unrelated last URL %q changed output", url)
			}
		}
	}
}

func TestBreadcrumbNestedLastURLInvariant(t *testing.T) {
	// R-WP9A-D906
	for _, count := range []int{2, 3, 5} {
		for _, tools := range []bool{false, true} {
			data := page.Banner{Tools: tools}
			for i := range count {
				data.Trail = append(data.Trail, page.Level{Name: fmt.Sprintf("level_%d", i), URL: fmt.Sprintf("/parent_%d", i)})
			}
			baseline := renderBreadcrumb(t, data)
			for _, url := range []string{"/", "/tools", "/about", "/consumer-other", "", "https://consumer.example.test/a?x=<>&y=two", "javascript:consumer(1)"} {
				data.Trail[len(data.Trail)-1].URL = url
				if renderBreadcrumb(t, data) != baseline {
					t.Fatalf("nested last URL %q changed output", url)
				}
			}
		}
	}
}
