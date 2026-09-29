// Package discourse ingests a Discourse forum (Chief Delphi) through its
// public RSS 2.0 feeds, adapter "discourse-rss" (docs/sources.md §5):
//
//   - src.URL is the topics feed (latest.rss): one item per recently active
//     topic, carrying the category and the topic's first post;
//   - src.PostsURL, optional, is the latest-posts feed (posts.rss): one item
//     per reply. It has no category, so a reply is kept only when its topic
//     is in the topics feed and passed src.Categories.
//
// Each post becomes one kind "forum" chunk (split only when it exceeds the
// chunk budget) with the source's trust (community) and license. Posts are
// user content and the main indirect-prompt-injection vector (T1 in
// docs/security.md): every text field is cleaned with internal/ingest/sanitize
// before and after HTML → Markdown conversion, and the suspect detector runs
// on title, heading and body. Items are deduplicated by GUID, and a topic's
// first post is taken from the topics feed only.
//
// Feeds are a rolling window: the shard mirrors what the forum shows now, so
// posts that moderators remove upstream disappear on the next build.
package discourse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/chunking"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/fetch"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/sanitize"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/sphinx"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

// Getter is the fetch dependency (a politeness-floored fetch.Fetcher in
// production).
type Getter interface {
	Get(ctx context.Context, url string) (*fetch.Result, error)
}

const (
	maxFeedBytes = 8 << 20 // a Discourse feed is ~100 KB; anything near this is not a feed
	maxItems     = 500     // per feed; Discourse serves 30 topics / 50 posts
)

// Stats summarizes a parse. Filtered counts items outside the allowed
// categories (or replies to topics the topics feed does not list).
type Stats struct {
	Topics, Replies, Chunks, Suspect     int
	Filtered, Duplicates, Invalid, Empty int
}

type feed struct {
	Channel struct {
		Items []item `xml:"item"`
	} `xml:"channel"`
}

type item struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        string   `xml:"guid"`
	PubDate     string   `xml:"pubDate"`
	Categories  []string `xml:"category"`
	Creator     string   `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Description string   `xml:"description"`
}

// post is one validated feed item.
type post struct {
	docID    string // <library>/topic-<id> | <library>/post-<id>, from the GUID
	topicID  string
	number   int    // post number within the topic (1 = the topic's first post)
	url      string // canonical permalink
	pub      time.Time
	title    string
	category string
	creator  string
	body     string // sanitized Markdown
}

var (
	guidRe     = regexp.MustCompile(`-(topic|post)-([0-9]{1,12})$`)
	topicPath  = regexp.MustCompile(`^/t/([^/]+)/([0-9]{1,12})(?:/([0-9]{1,6}))?/?$`)
	pageQuery  = regexp.MustCompile(`^page=[0-9]{1,6}$`)
	postAnchor = regexp.MustCompile(`^post_([0-9]{1,6})$`)
	username   = regexp.MustCompile(`^[\p{L}\p{N}_.-]{1,60}$`)
	footerRe   = regexp.MustCompile(`(?i)^(\d+ posts? - \d+ participants?|read full topic)$`)
)

// Parse reads the topics feed at topicsPath (and src.PostsURL through g) and
// emits one or more chunks per post.
func Parse(ctx context.Context, g Getter, topicsPath string, src sources.Source, retrieved time.Time,
	emit func(index.Chunk) error) (Stats, error) {
	var st Stats
	base, err := url.Parse(src.URL)
	if err != nil {
		return st, fmt.Errorf("discourse: %s: %w", src.URL, err)
	}
	topics, err := readFeed(topicsPath)
	if err != nil {
		return st, fmt.Errorf("discourse: %s: %w", src.URL, err)
	}
	seen := map[string]bool{}
	topicCategory := map[string]string{}
	for _, it := range topics {
		p, ok := parseItem(it, base.Host, src.Library, "topic")
		if !ok {
			st.Invalid++
			continue
		}
		if seen[p.docID] {
			st.Duplicates++
			continue
		}
		seen[p.docID] = true
		if !allowed(p.category, src.Categories) {
			st.Filtered++
			continue
		}
		topicCategory[p.topicID] = p.category
		ok, err := emitPost(p, src, retrieved, emit, &st)
		if err != nil {
			return st, err
		}
		if ok {
			st.Topics++
		}
	}
	if src.PostsURL == "" || g == nil {
		return st, nil
	}
	res, err := g.Get(ctx, src.PostsURL)
	if err != nil {
		return st, fmt.Errorf("discourse: %w", err)
	}
	replies, err := readFeed(res.Path)
	if err != nil {
		return st, fmt.Errorf("discourse: %s: %w", src.PostsURL, err)
	}
	for _, it := range replies {
		p, ok := parseItem(it, base.Host, src.Library, "post")
		if !ok {
			st.Invalid++
			continue
		}
		cat, known := topicCategory[p.topicID]
		switch {
		case seen[p.docID] || (known && p.number == 1): // the first post came with the topic
			st.Duplicates++
			continue
		case !known:
			st.Filtered++
			continue
		}
		seen[p.docID] = true
		p.category = cat
		ok, err := emitPost(p, src, retrieved, emit, &st)
		if err != nil {
			return st, err
		}
		if ok {
			st.Replies++
		}
	}
	return st, nil
}

func readFeed(path string) ([]item, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFeedBytes {
		return nil, fmt.Errorf("feed larger than %d bytes", maxFeedBytes)
	}
	var fd feed
	dec := xml.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&fd); err != nil {
		return nil, err
	}
	items := fd.Channel.Items
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	return items, nil
}

// parseItem validates one item. want is the GUID kind the feed carries
// ("topic" for latest.rss, "post" for posts.rss). Links must point at a topic
// on the feed's own host: the link becomes the citation.
func parseItem(it item, host, library, want string) (post, bool) {
	var p post
	m := guidRe.FindStringSubmatch(strings.TrimSpace(it.GUID))
	if m == nil || m[1] != want {
		return p, false
	}
	u, err := url.Parse(strings.TrimSpace(it.Link))
	// posts.rss links long topics with ?page=N; nothing else is expected.
	if err != nil || u.Scheme != "https" || u.Host != host || (u.RawQuery != "" && !pageQuery.MatchString(u.RawQuery)) {
		return p, false
	}
	lm := topicPath.FindStringSubmatch(u.Path)
	if lm == nil {
		return p, false
	}
	slug := lm[1]
	p.topicID, p.number = lm[2], 1
	if lm[3] != "" {
		p.number, _ = strconv.Atoi(lm[3])
	}
	if am := postAnchor.FindStringSubmatch(u.Fragment); am != nil {
		p.number, _ = strconv.Atoi(am[1])
	}
	if want == "topic" && m[2] != p.topicID {
		return p, false // GUID and link disagree about which topic this is
	}
	p.pub, err = time.Parse(time.RFC1123Z, strings.TrimSpace(it.PubDate))
	if err != nil {
		if p.pub, err = time.Parse(time.RFC1123, strings.TrimSpace(it.PubDate)); err != nil {
			return p, false
		}
	}
	p.pub = p.pub.UTC()
	// Cite the canonical permalink: /t/<slug>/<topic> for a first post,
	// /t/<slug>/<topic>/<n> for a reply (stable when the page count grows).
	u.RawQuery, u.RawPath, u.Fragment = "", "", ""
	u.Path = "/t/" + slug + "/" + p.topicID
	if p.number > 1 {
		u.Path += "/" + strconv.Itoa(p.number)
	}
	p.url = u.String()
	p.docID = library + "/" + m[1] + "-" + m[2]
	p.title = line(it.Title)
	if p.title == "" {
		return p, false
	}
	for _, c := range it.Categories {
		if c = line(c); c != "" {
			p.category = c
			break
		}
	}
	// posts.rss renders "@username Display Name"; keep the username only
	// (the attribution the content license requires, nothing more).
	if f := strings.Fields(line(it.Creator)); len(f) > 0 {
		if name := strings.TrimPrefix(f[0], "@"); username.MatchString(name) {
			p.creator = name
		}
	}
	p.body = Markdown(it.Description)
	return p, true
}

// emitPost emits a post's chunks; false means it had no text of its own.
func emitPost(p post, src sources.Source, retrieved time.Time, emit func(index.Chunk) error, st *Stats) (bool, error) {
	if textutil.EstimateTokens(p.body) < 2 {
		st.Empty++ // image-only or quote-only posts
		return false, nil
	}
	heading := siteName(src.Library)
	if p.category != "" {
		heading += " › " + p.category
	}
	switch {
	case p.number > 1 && p.creator != "":
		heading += " › reply #" + strconv.Itoa(p.number) + " by @" + p.creator
	case p.number > 1:
		heading += " › reply #" + strconv.Itoa(p.number)
	case p.creator != "":
		heading += " › @" + p.creator
	}
	season := strconv.Itoa(p.pub.Year()) // FRC seasons run January to December
	sum := sha256.Sum256([]byte(p.title + "\n" + p.body))
	rev := "sha256:" + hex.EncodeToString(sum[:])[:16]
	suspect := sanitize.Suspect(p.title) || sanitize.Suspect(heading) || sanitize.Suspect(p.body)
	if suspect {
		st.Suspect++
	}
	for ord, part := range chunking.Split(p.body) {
		if err := emit(index.Chunk{DocID: p.docID, Ord: ord, Library: src.Library, VersionLo: season, Season: season,
			Channel: "stable", Language: language(p.category), Kind: "forum", Title: p.title, HeadingPath: heading,
			Body: part, SourceURL: p.url, UpstreamRev: rev, RetrievedAt: retrieved,
			License: src.License, Trust: src.Trust, Suspect: suspect}); err != nil {
			return false, err
		}
		st.Chunks++
	}
	return true, nil
}

func allowed(category string, keep []string) bool {
	if len(keep) == 0 {
		return true
	}
	for _, k := range keep {
		if strings.EqualFold(k, category) {
			return true
		}
	}
	return false
}

// language maps the language-specific subcategories; everything else is
// language-neutral, so language-filtered searches still see it.
func language(category string) string {
	switch strings.ToLower(category) {
	case "java":
		return "java"
	case "c/c++":
		return "cpp"
	case "python":
		return "python"
	}
	return "any"
}

func siteName(library string) string {
	if library == "chiefdelphi" {
		return "Chief Delphi"
	}
	return library
}

// line cleans a single-line text field (title, category, creator).
func line(s string) string {
	return strings.Join(strings.Fields(sanitize.Clean(s)), " ")
}

// Markdown converts a post's cooked HTML to sanitized Markdown. Hidden
// elements are dropped on the parsed DOM (hidden attribute, display:none and
// similar styles, script/style/template), where nesting cannot fool a regular
// expression; comments never render; sanitize.Clean then strips invisible
// characters from the result. Discourse chrome that is not the author's text
// is dropped too: quotes of other posts, link previews (kept as the bare
// link), image lightboxes and the topics feed's "N posts - M participants" /
// "Read full topic" footer.
func Markdown(raw string) string {
	parent := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(raw), parent)
	if err != nil {
		return ""
	}
	box := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		box.AppendChild(n)
	}
	pruneChildren(box)
	var parts []string
	for c := box.FirstChild; c != nil; c = c.NextSibling {
		parts = append(parts, render(c)...)
	}
	return strings.TrimSpace(sanitize.Clean(strings.Join(parts, "\n\n")))
}

// render converts one top-level node. Headings and Discourse code blocks are
// handled here (the Sphinx converter expects them in its own page structure);
// everything else goes through sphinx.Markdown.
func render(n *html.Node) []string {
	if n.Type != html.ElementNode {
		return sphinx.Markdown(n)
	}
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		if t := sphinx.InlineText(n); t != "" {
			return []string{"**" + t + "**"}
		}
		return nil
	case atom.Pre:
		lang := ""
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.DataAtom == atom.Code {
				for _, cls := range strings.Fields(attr(c, "class")) {
					if l, ok := strings.CutPrefix(cls, "lang-"); ok && l != "auto" && l != "plaintext" && l != "nohighlight" {
						lang = strings.ReplaceAll(l, "c++", "cpp")
					}
				}
			}
		}
		return []string{"```" + lang + "\n" + strings.TrimRight(textOf(n), "\n") + "\n```"}
	case atom.P:
		if footerRe.MatchString(sphinx.InlineText(n)) {
			return nil
		}
	}
	return sphinx.Markdown(n)
}

// pruneChildren removes hidden and non-author subtrees below n in place.
func pruneChildren(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch {
		case c.Type != html.ElementNode:
		case drop(c):
			n.RemoveChild(c)
		case c.DataAtom == atom.Aside && hasClass(c, "onebox"):
			// Link previews quote a third-party page: keep only the link.
			if href := attr(c, "data-onebox-src"); strings.HasPrefix(href, "https://") {
				p := &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P}
				p.AppendChild(&html.Node{Type: html.TextNode, Data: href})
				n.InsertBefore(p, c)
			}
			n.RemoveChild(c)
		default:
			pruneChildren(c)
		}
		c = next
	}
}

func drop(n *html.Node) bool {
	if _, hidden := attrOK(n, "hidden"); hidden {
		return true
	}
	style := strings.ToLower(strings.ReplaceAll(attr(n, "style"), " ", ""))
	for _, s := range []string{"display:none", "visibility:hidden", "font-size:0", "opacity:0"} {
		if strings.Contains(style, s) {
			return true
		}
	}
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Template, atom.Noscript, atom.Img, atom.Svg, atom.Iframe:
		return true
	case atom.Aside:
		return hasClass(n, "quote")
	}
	return hasClass(n, "lightbox-wrapper") || hasClass(n, "hidden")
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func attrOK(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func attr(n *html.Node, key string) string {
	v, _ := attrOK(n, key)
	return v
}

func hasClass(n *html.Node, c string) bool {
	for _, x := range strings.Fields(attr(n, "class")) {
		if x == c {
			return true
		}
	}
	return false
}
