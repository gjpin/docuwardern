package scrape

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
	"github.com/zero/docuwarden/corpus"
	docmarkdown "github.com/zero/docuwarden/markdown"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

type Config struct {
	Source     corpus.SourceSpec
	Workers    int
	Throttle   time.Duration
	Timeout    time.Duration
	MaxRetries int
	Backoff    time.Duration
	UserAgent  string
	HTTPClient *http.Client
	Converter  docmarkdown.Converter
	Now        func() time.Time
	Sleep      func(context.Context, time.Duration) error
	Progress   func(format string, args ...any)
}

type CrawlError struct{ Artifact corpus.Artifact }

func (e *CrawlError) Error() string {
	return fmt.Sprintf("crawl incomplete: %d failed, %d missing content selector", len(e.Artifact.Report.Failed), len(e.Artifact.Report.SelectorMissing))
}

type crawler struct {
	cfg       Config
	scope     Scope
	client    *http.Client
	throttleM sync.Mutex
	lastFetch map[string]time.Time
}

type pageResult struct {
	requested string
	finalURL  string
	status    int
	title     string
	markdown  string
	links     []string
	redirects []corpus.PageEvent
	skipped   []corpus.PageEvent
	missing   bool
	ignored   bool
	err       error
}

type outsideScopeRedirectError struct {
	target string
}

func (err *outsideScopeRedirectError) Error() string {
	return "redirect outside seed scope: " + err.target
}

func Crawl(ctx context.Context, cfg Config) (corpus.Artifact, error) {
	return CrawlTargets(ctx, cfg, nil, nil)
}

// CrawlTargets crawls initial URLs and any links they discover, without
// refetching canonical URLs already represented by successful documents.
func CrawlTargets(ctx context.Context, cfg Config, initialURLs, knownSuccessfulURLs []string) (corpus.Artifact, error) {
	if cfg.Source.SourceID == "" || cfg.Source.SeedURL == "" || cfg.Source.ContentSelector == "" {
		return corpus.Artifact{}, errors.New("source ID, seed URL, and content selector are required")
	}
	for _, selector := range contentSelectors(cfg.Source) {
		if _, err := cascadia.Parse(selector); err != nil {
			return corpus.Artifact{}, fmt.Errorf("invalid content selector %q: %w", selector, err)
		}
	}
	scope, seed, err := NewScope(cfg.Source.SeedURL)
	if err != nil {
		return corpus.Artifact{}, err
	}
	cfg.Source.SeedURL = seed
	defaults(&cfg)
	c := &crawler{cfg: cfg, scope: scope, client: cfg.HTTPClient, lastFetch: map[string]time.Time{}}
	started := cfg.Now().UTC()
	artifact := corpus.Artifact{Manifest: corpus.Manifest{SchemaVersion: corpus.SchemaVersion, Source: cfg.Source, StartedAt: started, Crawl: settings(cfg)}, Markdown: map[string]string{}}
	seen := map[string]bool{}
	documented := map[string]bool{}
	for _, rawURL := range knownSuccessfulURLs {
		canonical, accepted, resolveErr := scope.Resolve(seed, rawURL)
		if resolveErr == nil && accepted {
			seen[canonical] = true
			documented[canonical] = true
		}
	}
	if len(initialURLs) == 0 {
		initialURLs = []string{seed}
	}
	var queue []string
	queued := map[string]bool{}
	for _, rawURL := range initialURLs {
		canonical, accepted, resolveErr := scope.Resolve(seed, rawURL)
		if resolveErr != nil {
			return corpus.Artifact{}, fmt.Errorf("invalid initial URL %q: %w", rawURL, resolveErr)
		}
		if !accepted {
			return corpus.Artifact{}, fmt.Errorf("initial URL outside seed scope: %s", rawURL)
		}
		if !queued[canonical] {
			queued[canonical] = true
			seen[canonical] = true
			queue = append(queue, canonical)
		}
	}
	sort.Strings(queue)
	processed := 0
	lastProgressBucket := -1
	for len(queue) > 0 {
		batch := queue
		queue = nil
		c.fetchBatch(ctx, batch, func(result pageResult) {
			seen[result.finalURL] = true
			artifact.Report.Redirected = append(artifact.Report.Redirected, result.redirects...)
			artifact.Report.Skipped = append(artifact.Report.Skipped, result.skipped...)
			if result.ignored {
				// No document is produced, but the discovered page was processed.
			} else if result.err != nil {
				artifact.Report.Failed = append(artifact.Report.Failed, corpus.PageEvent{URL: result.requested, StatusCode: result.status, Detail: result.err.Error()})
			} else if result.missing {
				artifact.Report.SelectorMissing = append(artifact.Report.SelectorMissing, corpus.PageEvent{URL: result.finalURL, StatusCode: result.status, Detail: "content selector did not match"})
			} else if documented[result.finalURL] {
				artifact.Report.Skipped = append(artifact.Report.Skipped, corpus.PageEvent{URL: result.requested, Target: result.finalURL, Detail: "duplicate canonical page"})
			} else {
				documented[result.finalURL] = true
				crawledAt := cfg.Now().UTC()
				id := corpus.DocumentID(cfg.Source.SourceID, cfg.Source.Version, result.finalURL)
				doc := corpus.Document{ID: id, URL: result.finalURL, Title: result.title, Filename: "documents/" + corpus.FilenameFor(id), ContentHash: corpus.HashString(result.markdown), CrawledAt: crawledAt}
				artifact.Manifest.Documents = append(artifact.Manifest.Documents, doc)
				artifact.Markdown[id] = result.markdown
				artifact.Report.Fetched = append(artifact.Report.Fetched, corpus.PageEvent{URL: result.finalURL, StatusCode: result.status})
			}
			for _, link := range result.links {
				if !seen[link] {
					seen[link] = true
					queue = append(queue, link)
				}
			}
			processed++
			progressBucket := (processed * 100 / len(seen)) / 10 * 10
			if cfg.Progress != nil && progressBucket >= 10 && progressBucket != lastProgressBucket {
				cfg.Progress("crawl: fetching %d%% (%d/%d discovered pages processed)", progressBucket, processed, len(seen))
				lastProgressBucket = progressBucket
			}
		})
		sort.Strings(queue)
		if err := ctx.Err(); err != nil {
			artifact.Report.Failed = append(artifact.Report.Failed, corpus.PageEvent{URL: seed, Detail: err.Error()})
			break
		}
	}
	artifact.Manifest.CompletedAt = cfg.Now().UTC()
	artifact.Manifest.Complete = len(artifact.Report.Failed) == 0 && len(artifact.Report.SelectorMissing) == 0
	corpus.Sort(&artifact)
	if !artifact.Manifest.Complete {
		return artifact, &CrawlError{Artifact: artifact}
	}
	return artifact, nil
}

func settings(cfg Config) corpus.CrawlSettings {
	return corpus.CrawlSettings{Workers: cfg.Workers, Throttle: cfg.Throttle, Timeout: cfg.Timeout, MaxRetries: cfg.MaxRetries, Backoff: cfg.Backoff}
}

func contentSelectors(source corpus.SourceSpec) []string {
	selectors := []string{source.ContentSelector}
	selectors = append(selectors, source.ContentSelectors...)
	return selectors
}

func defaults(cfg *Config) {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 3
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = 200 * time.Millisecond
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "docuwarden/1.0 (+https://github.com/zero/docuwarden)"
	}
	if cfg.Converter == nil {
		cfg.Converter = docmarkdown.HTMLConverter{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepContext
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}
}

func (c *crawler) fetchBatch(ctx context.Context, urls []string, consume func(pageResult)) {
	jobs := make(chan int)
	results := make(chan pageResult)
	var wg sync.WaitGroup
	workers := min(c.cfg.Workers, len(urls))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results <- c.fetchPage(ctx, urls[index])
			}
		}()
	}
	go func() {
		for index := range urls {
			jobs <- index
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	for result := range results {
		consume(result)
	}
}

func (c *crawler) fetchPage(ctx context.Context, pageURL string) pageResult {
	result := pageResult{requested: pageURL, finalURL: pageURL}
	var response *http.Response
	var err error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := c.cfg.Sleep(ctx, c.cfg.Backoff*time.Duration(1<<(attempt-1))); err != nil {
				result.err = err
				return result
			}
		}
		if err := c.waitForHost(ctx, pageURL); err != nil {
			result.err = err
			return result
		}
		response, err = c.do(ctx, pageURL, &result)
		var outsideScope *outsideScopeRedirectError
		if errors.As(err, &outsideScope) {
			break
		}
		if err == nil && response.StatusCode != http.StatusTooManyRequests && response.StatusCode < 500 {
			break
		}
		if response != nil {
			response.Body.Close()
		}
		if attempt == c.cfg.MaxRetries {
			break
		}
	}
	if err != nil {
		var outsideScope *outsideScopeRedirectError
		if errors.As(err, &outsideScope) {
			result.skipped = append(result.skipped, corpus.PageEvent{URL: pageURL, Target: outsideScope.target, Detail: "redirect outside seed scope"})
			result.ignored = true
			return result
		}
		result.err = fmt.Errorf("fetch: %w", err)
		return result
	}
	result.status = response.StatusCode
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.err = fmt.Errorf("HTTP status %s", response.Status)
		return result
	}
	contentType := response.Header.Get("Content-Type")
	mediaType, decodeContentType, responseBody, htmlResponse := classifyResponseMediaType(response.Body, contentType)
	if !htmlResponse {
		event := corpus.PageEvent{URL: result.requested, StatusCode: result.status, Detail: fmt.Sprintf("non-HTML response: %q", mediaType)}
		if result.finalURL != result.requested {
			event.Target = result.finalURL
		}
		if result.requested == c.cfg.Source.SeedURL {
			result.err = errors.New(event.Detail)
		} else {
			result.skipped = append(result.skipped, event)
			result.ignored = true
		}
		return result
	}
	utf8Reader, err := charset.NewReader(responseBody, decodeContentType)
	if err != nil {
		result.err = fmt.Errorf("decode response charset: %w", err)
		return result
	}
	body, err := io.ReadAll(utf8Reader)
	if err != nil {
		result.err = fmt.Errorf("read response: %w", err)
		return result
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		result.err = fmt.Errorf("parse HTML: %w", err)
		return result
	}
	if target, ok := immediateMetaRefreshTarget(doc); ok {
		resolved, accepted, resolveErr := c.scope.Resolve(result.finalURL, target)
		if resolveErr == nil && resolved != "" {
			if !accepted {
				result.skipped = append(result.skipped, corpus.PageEvent{URL: result.finalURL, Target: resolved, Detail: "redirect outside seed scope"})
				result.ignored = true
				return result
			}
			result.redirects = append(result.redirects, corpus.PageEvent{URL: result.finalURL, Target: resolved, StatusCode: result.status, Detail: "HTML meta refresh"})
			result.links = []string{resolved}
			result.ignored = true
			return result
		}
	}
	result.title = strings.TrimSpace(doc.Find("title").First().Text())
	links := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		resolved, accepted, resolveErr := c.scope.Resolve(result.finalURL, href)
		if resolveErr != nil || resolved == "" || links[resolved] {
			return
		}
		links[resolved] = accepted
		if !accepted {
			result.skipped = append(result.skipped, corpus.PageEvent{URL: resolved, Detail: "outside seed scope"})
		}
	})
	for link, accepted := range links {
		if accepted {
			result.links = append(result.links, link)
		}
	}
	sort.Strings(result.links)
	var selection *goquery.Selection
	for _, selector := range contentSelectors(c.cfg.Source) {
		candidate := doc.Find(selector).First()
		if candidate.Length() > 0 {
			selection = candidate
			break
		}
	}
	if selection == nil {
		result.missing = true
		return result
	}
	fragment, err := selectionHTML(selection)
	if err != nil {
		result.err = err
		return result
	}
	result.markdown, err = c.cfg.Converter.Convert(ctx, result.finalURL, strings.NewReader(fragment))
	if err != nil {
		result.err = err
		return result
	}
	return result
}

func immediateMetaRefreshTarget(doc *goquery.Document) (string, bool) {
	var target string
	doc.Find("meta[http-equiv][content]").EachWithBreak(func(_ int, meta *goquery.Selection) bool {
		httpEquiv, _ := meta.Attr("http-equiv")
		if !strings.EqualFold(strings.TrimSpace(httpEquiv), "refresh") {
			return true
		}
		content, _ := meta.Attr("content")
		delay, directive, found := strings.Cut(content, ";")
		if !found {
			return true
		}
		seconds, err := strconv.ParseFloat(strings.TrimSpace(delay), 64)
		if err != nil || seconds != 0 {
			return true
		}
		name, value, found := strings.Cut(directive, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(name), "url") {
			return true
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = strings.TrimSpace(value[1 : len(value)-1])
		} else if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			return true
		}
		if value == "" {
			return true
		}
		target = value
		return false
	})
	return target, target != ""
}

func classifyResponseMediaType(body io.Reader, contentType string) (string, string, io.Reader, bool) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	mediaType = strings.ToLower(mediaType)
	if err == nil && mediaType != "application/octet-stream" {
		return mediaType, contentType, body, mediaType == "text/html" || mediaType == "application/xhtml+xml"
	}

	buffered := bufio.NewReaderSize(body, 512)
	prefix, _ := buffered.Peek(512)
	detected, _, _ := mime.ParseMediaType(http.DetectContentType(prefix))
	detected = strings.ToLower(detected)
	return detected, detected, buffered, detected == "text/html" || detected == "application/xhtml+xml"
}

func (c *crawler) do(ctx context.Context, pageURL string, result *pageResult) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "text/html, application/xhtml+xml")
	client := *c.client
	baseCheck := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if baseCheck != nil {
			if err := baseCheck(req, via); err != nil {
				return err
			}
		}
		canonical, accepted, err := c.scope.Resolve(via[len(via)-1].URL.String(), req.URL.String())
		if err != nil || !accepted {
			return &outsideScopeRedirectError{target: req.URL.String()}
		}
		result.redirects = append(result.redirects, corpus.PageEvent{URL: via[len(via)-1].URL.String(), Target: canonical, StatusCode: req.Response.StatusCode})
		result.finalURL = canonical
		return nil
	}
	return client.Do(req)
}

func (c *crawler) waitForHost(ctx context.Context, rawURL string) error {
	if c.cfg.Throttle <= 0 {
		return nil
	}
	u, _ := url.Parse(rawURL)
	c.throttleM.Lock()
	wait := c.cfg.Throttle - c.cfg.Now().Sub(c.lastFetch[u.Host])
	if wait < 0 {
		wait = 0
	}
	c.lastFetch[u.Host] = c.cfg.Now().Add(wait)
	c.throttleM.Unlock()
	return c.cfg.Sleep(ctx, wait)
}

func selectionHTML(selection *goquery.Selection) (string, error) {
	var out strings.Builder
	for _, node := range selection.Nodes {
		if err := html.Render(&out, node); err != nil {
			return "", fmt.Errorf("render selected HTML: %w", err)
		}
	}
	return out.String(), nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
