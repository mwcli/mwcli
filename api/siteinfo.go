package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Site describes a wiki: its general information, statistics and installed
// extensions (meta=siteinfo). JSON names follow the MediaWiki API.
type Site struct {
	SiteName    string      `json:"sitename"`
	MainPage    string      `json:"mainpage"`
	URL         string      `json:"url"`
	Server      string      `json:"server"`
	ArticlePath string      `json:"articlepath"`
	ScriptPath  string      `json:"scriptpath"`
	Lang        string      `json:"lang"`
	Generator   string      `json:"generator"`
	PHPVersion  string      `json:"phpversion"`
	DBType      string      `json:"dbtype"`
	DBVersion   string      `json:"dbversion"`
	Timezone    string      `json:"timezone"`
	ReadOnly    bool        `json:"readonly"`
	Statistics  Statistics  `json:"statistics"`
	Extensions  []Extension `json:"extensions"`
}

// Statistics are the counts of a wiki.
type Statistics struct {
	Pages       int `json:"pages"`
	Articles    int `json:"articles"`
	Edits       int `json:"edits"`
	Images      int `json:"images"`
	Users       int `json:"users"`
	ActiveUsers int `json:"activeusers"`
	Admins      int `json:"admins"`
}

// Extension is an installed extension or skin.
type Extension struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Version string `json:"version"`
	URL     string `json:"url"`
	License string `json:"license"`
}

// SiteOptions picks the optional parts of SiteDetails.
type SiteOptions struct {
	Statistics, Extensions bool
}

// SiteDetails returns the general information of the wiki, with its
// statistics and extensions when asked for.
func (c *Client) SiteDetails(ctx context.Context, opts SiteOptions) (*Site, error) {
	props := []string{"general"}
	if opts.Statistics {
		props = append(props, "statistics")
	}
	if opts.Extensions {
		props = append(props, "extensions")
	}
	var result struct {
		Query struct {
			General struct {
				SiteName    string `json:"sitename"`
				MainPage    string `json:"mainpage"`
				Base        string `json:"base"`
				Server      string `json:"server"`
				ArticlePath string `json:"articlepath"`
				ScriptPath  string `json:"scriptpath"`
				Lang        string `json:"lang"`
				Generator   string `json:"generator"`
				PHPVersion  string `json:"phpversion"`
				DBType      string `json:"dbtype"`
				DBVersion   string `json:"dbversion"`
				Timezone    string `json:"timezone"`
				ReadOnly    bool   `json:"readonly"`
			} `json:"general"`
			Statistics Statistics `json:"statistics"`
			Extensions []struct {
				Extension
				LicenseName string `json:"license-name"`
			} `json:"extensions"`
		} `json:"query"`
	}
	params := url.Values{"action": {"query"}, "meta": {"siteinfo"}, "siprop": {strings.Join(props, "|")}}
	if err := c.request(ctx, http.MethodGet, params, &result); err != nil {
		return nil, err
	}
	g := result.Query.General
	if !strings.HasPrefix(g.Generator, "MediaWiki") {
		return nil, fmt.Errorf("%s is not a MediaWiki API", c.API)
	}
	s := &Site{
		SiteName: g.SiteName, MainPage: g.MainPage, URL: g.Base, Server: g.Server,
		ArticlePath: g.ArticlePath, ScriptPath: g.ScriptPath, Lang: g.Lang,
		Generator: g.Generator, PHPVersion: g.PHPVersion, DBType: g.DBType,
		DBVersion: g.DBVersion, Timezone: g.Timezone, ReadOnly: g.ReadOnly,
		Statistics: result.Query.Statistics, Extensions: []Extension{},
	}
	for _, e := range result.Query.Extensions {
		x := e.Extension
		x.License = e.LicenseName
		s.Extensions = append(s.Extensions, x)
	}
	return s, nil
}
