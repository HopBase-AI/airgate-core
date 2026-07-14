package blog

import (
	"strings"
	"testing"
)

// TestSanitizeHTML 覆盖存储型 XSS 防护与富文本白名单。
func TestSanitizeHTML(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		mustContain []string
		mustReject  []string
	}{
		{
			name:       "strip script tag",
			in:         `<p>hello</p><script>alert(1)</script>`,
			mustContain: []string{"<p>hello</p>"},
			mustReject:  []string{"<script", "alert(1)"},
		},
		{
			name:       "strip onerror handler",
			in:         `<img src="/a.png" onerror="alert(1)">`,
			mustReject: []string{"onerror", "alert(1)"},
		},
		{
			name:       "strip javascript href",
			in:         `<a href="javascript:alert(1)">x</a>`,
			mustReject: []string{"javascript:"},
		},
		{
			name:        "keep basic formatting",
			in:          `<strong>b</strong><em>i</em><h2>t</h2><ul><li>a</li></ul>`,
			mustContain: []string{"<strong>b</strong>", "<em>i</em>", "<h2>t</h2>", "<li>a</li>"},
		},
		{
			name:        "keep image with src/alt",
			in:          `<img src="/assets-runtime/x.png" alt="pic">`,
			mustContain: []string{`src="/assets-runtime/x.png"`, `alt="pic"`},
		},
		{
			name:        "keep youtube iframe",
			in:          `<iframe src="https://www.youtube.com/embed/abc123" allowfullscreen></iframe>`,
			mustContain: []string{"<iframe", "https://www.youtube.com/embed/abc123"},
		},
		{
			name:        "keep bilibili iframe",
			in:          `<iframe src="https://player.bilibili.com/player.html?bvid=BV1"></iframe>`,
			mustContain: []string{"player.bilibili.com/player.html"},
		},
		{
			name:       "strip untrusted iframe src",
			in:         `<iframe src="https://evil.example.com/phish"></iframe>`,
			mustReject: []string{"evil.example.com"},
		},
		{
			name:        "keep text-align style",
			in:          `<p style="text-align:center">x</p>`,
			mustContain: []string{"text-align", "center"},
		},
		{
			name:       "strip non-whitelisted style",
			in:         `<p style="position:fixed;color:red">x</p>`,
			mustReject: []string{"position:fixed", "color:red"},
		},
		{
			name:        "keep class attribute",
			in:          `<pre class="language-go"><code>x</code></pre>`,
			mustContain: []string{`class="language-go"`, "<code>x</code>"},
		},
		{
			name:       "strip data html image",
			in:         `<img src="data:text/html,<script>alert(1)</script>">`,
			mustReject: []string{"data:text/html", "alert(1)"},
		},
		{
			name:        "empty input stays empty",
			in:          ``,
			mustContain: nil,
			mustReject:  []string{"<script"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeHTML(tc.in)
			for _, want := range tc.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("sanitized output missing %q\n got: %s", want, got)
				}
			}
			for _, reject := range tc.mustReject {
				if strings.Contains(got, reject) {
					t.Errorf("sanitized output should not contain %q\n got: %s", reject, got)
				}
			}
		})
	}
}

// TestSanitizeHTML_AdversarialIframe 针对 iframe host 白名单的绕过尝试。
func TestSanitizeHTML_AdversarialIframe(t *testing.T) {
	// 这些 src 都不应通过白名单(host 前缀伪造/协议相对/大小写/凭证注入)。
	rejected := []string{
		`<iframe src="https://www.youtube.com.evil.com/embed/x"></iframe>`,
		`<iframe src="https://www.youtube.com@evil.com/embed/x"></iframe>`,
		`<iframe src="//www.youtube.com/embed/x"></iframe>`,
		`<iframe src="HTTPS://www.youtube.com/embed/x"></iframe>`,
		`<iframe src="http://www.youtube.com/embed/x"></iframe>`,
		`<iframe src="javascript:alert(1)"></iframe>`,
		`<iframe srcdoc="<script>alert(1)</script>"></iframe>`,
	}
	for _, in := range rejected {
		got := SanitizeHTML(in)
		for _, bad := range []string{"evil.com", "javascript:", "srcdoc", "alert(1)", "//www.youtube.com"} {
			if strings.Contains(got, bad) {
				t.Errorf("input %q leaked %q\n got: %s", in, bad, got)
			}
		}
	}

	// 合法的受信任视频 host 应通过。
	accepted := []struct{ in, want string }{
		{`<iframe src="https://www.youtube.com/embed/abc"></iframe>`, "youtube.com/embed/abc"},
		{`<iframe src="https://www.youtube-nocookie.com/embed/abc"></iframe>`, "youtube-nocookie.com/embed/abc"},
		{`<iframe src="https://player.vimeo.com/video/123"></iframe>`, "player.vimeo.com/video/123"},
	}
	for _, tc := range accepted {
		got := SanitizeHTML(tc.in)
		if !strings.Contains(got, tc.want) {
			t.Errorf("trusted iframe dropped: input %q\n got: %s", tc.in, got)
		}
	}

	// data:image/svg 内嵌脚本应被剥除(仅 data:text/html 在主表已测)。
	svg := SanitizeHTML(`<img src="data:image/svg+xml,<svg onload=alert(1)>">`)
	if strings.Contains(svg, "onload") || strings.Contains(svg, "alert(1)") {
		t.Errorf("data:svg with script not sanitized: %s", svg)
	}
}
