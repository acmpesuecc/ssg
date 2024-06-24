package parser

import (
	"bytes"
	"html/template"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/anna-ssg/anna/v2/pkg/helpers"
	figure "github.com/mangoumbrella/goldmark-figure"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"go.abhg.dev/goldmark/anchor"
	"go.abhg.dev/goldmark/mermaid"
	"go.abhg.dev/goldmark/toc"
	"gopkg.in/yaml.v3"
)

type LayoutConfig struct {
	Navbar      []map[string]string `yaml:"navbar"`
	BaseURL     string              `yaml:"baseURL"`
	SiteTitle   string              `yaml:"siteTitle"`
	SiteScripts []string            `yaml:"siteScripts"`
	Author      string              `yaml:"author"`
	Copyright   string              `yaml:"copyright"`
	ThemeURL    string              `yaml:"themeURL"`
}

type Frontmatter struct {
	Title        string   `yaml:"title"`
	Date         string   `yaml:"date"`
	Draft        bool     `yaml:"draft"`
	JSFiles      []string `yaml:"scripts"`
	Type         string   `yaml:"type"`
	Description  string   `yaml:"description"`
	PreviewImage string   `yaml:"previewimage"`
	Tags         []string `yaml:"tags"`
	TOC          bool     `yaml:"toc"`
	Authors      []string `yaml:"authors"`
	Collections  []string `yaml:"collections"`
	Layout       string   `yaml:"layout"`

	// Head is specifically used for
	// mentioning the head of the notes
	Head bool `yaml:"head"`
}

// type Sushi struct {
// 	// collections: ["homebrew", "workshop"]
// 	// 					0			1
// 	child_collection   string
// 	parsent_collection string
// }

// TemplateData This struct holds all of the data required to render any page of the site
type TemplateData struct {
	CompleteURL template.URL
	Date        int64
	Frontmatter Frontmatter
	Body        template.HTML
	LiveReload  bool
}

type Date int64

type Parser struct {
	// Templates stores the template data of all the pages of the site
	// Access the data for a particular page by using the relative path to the file as the key
	Templates map[template.URL]TemplateData

	// K-V pair storing all templates correspoding to a particular tag in the site
	TagsMap map[template.URL][]TemplateData

	// Collections stores template data of files in collections
	CollectionsMap map[template.URL][]TemplateData

	// Stores data parsed from layout/config.yml
	LayoutConfig LayoutConfig

	// Posts contains the template data of files in the posts directory
	Posts []TemplateData

	// Stores all the notes
	Notes map[template.URL]Note

	// TODO: Look into the two below fields into a single one
	MdFilesName []string
	MdFilesPath []string

	// Stores flag value to render draft posts
	RenderDrafts bool

	// Common logger for all parser functions
	ErrorLogger *log.Logger

	Helper *helpers.Helper

	// Determines the injection of Live Reload JS in HTML
	LiveReload bool

	// The path to the directory being rendered
	SiteDataPath string
}

func (p *Parser) ParseMDDir(baseDirPath string, baseDirFS fs.FS) {
	helper := helpers.Helper{
		ErrorLogger: p.ErrorLogger,
	}
	err := fs.WalkDir(baseDirFS, ".", func(path string, dir fs.DirEntry, err error) error {
		if path != "." && path != ".obsidian" {
			if dir.IsDir() {
				subDir := os.DirFS(path)
				p.ParseMDDir(path, subDir)
			} else {
				fileName := strings.TrimPrefix(path, baseDirPath)
				if filepath.Ext(path) == ".md" {
					content, err := os.ReadFile(baseDirPath + path)
					if err != nil {
						p.ErrorLogger.Fatal(err)
					}

					fronmatter, body, markdownContent, parseSuccess := p.ParseMarkdownContent(string(content))
					if parseSuccess {
						if fronmatter.Type == "post" {
							if (fronmatter.Draft && p.RenderDrafts) || !fronmatter.Draft {
								p.AddFile(baseDirPath, fileName, fronmatter, markdownContent, body)
							}
						} else {
							p.AddFile(baseDirPath, fileName, fronmatter, markdownContent, body)
						}
					}
				} else {
					helper.CopyFiles(p.SiteDataPath+"content/"+fileName, p.SiteDataPath+"rendered/"+fileName)
				}
			}
		}
		return nil
	})
	if err != nil {
		helper.ErrorLogger.Fatal(err)
	}
}

func (p *Parser) AddFile(baseDirPath string, dirEntryPath string, frontmatter Frontmatter, markdownContent string, body string) {
	p.MdFilesName = append(p.MdFilesName, dirEntryPath)
	testFilepath := baseDirPath + dirEntryPath
	p.MdFilesPath = append(p.MdFilesPath, testFilepath)

	var date int64
	if frontmatter.Date != "" {
		date = p.DateParse(frontmatter.Date).Unix()
	} else {
		date = 0
	}

	key, _ := strings.CutPrefix(testFilepath, p.SiteDataPath+"content/")
	url, _ := strings.CutSuffix(key, ".md")
	url += ".html"

	if frontmatter.Type == "post" || frontmatter.Type == "page" {

		page := TemplateData{
			CompleteURL: template.URL(url),
			Date:        date,
			Frontmatter: frontmatter,
			Body:        template.HTML(body),
			LiveReload:  p.LiveReload,
		}

		// Adding the page to the merged map storing all site pages
		if frontmatter.Type == "post" {
			p.Posts = append(p.Posts, page)
		}

		p.Templates[template.URL(url)] = page

		// Adding the page to the tags map with the corresponding tags
		for _, tag := range page.Frontmatter.Tags {
			tagsMapKey := "tags/" + tag + ".html"
			p.TagsMap[template.URL(tagsMapKey)] = append(p.TagsMap[template.URL(tagsMapKey)], page)

		}

		// Adding the page to the collections map with the corresponding collections
		for _, collection := range page.Frontmatter.Collections {
			collectionsMapKey := "collections/" + collection + ".html"
			p.CollectionsMap[template.URL(collectionsMapKey)] = append(p.CollectionsMap[template.URL(collectionsMapKey)], page)

		}

	}

	if frontmatter.Type == "note" {
		markdownContent = strings.TrimFunc(markdownContent, func(r rune) bool {
			return r == '\n' || r == '\t'
		})

		// trim the content up to n characters

		if len(markdownContent) > 200 {
			markdownContent = markdownContent[:200]
		}

		note := Note{
			CompleteURL:  template.URL(url),
			Date:         date,
			Frontmatter:  frontmatter,
			Body:         template.HTML(body),
			MarkdownBody: markdownContent,
			// preallocating the slice
			LinkedNoteURLs: make([]template.URL, 0, 5),
			LiveReload:     p.LiveReload,
		}

		p.Notes[note.CompleteURL] = note

		// NOTE: not adding the template urls of referenced ntoes
		// rather, will populate it while links
	}
}

func (p *Parser) ParseMarkdownContent(filecontent string) (Frontmatter, string, string, bool) {
	var parsedFrontmatter Frontmatter
	var markdown string
	/*
	   ---
	   frontmatter_content
	   ---

	   markdown content
	   --- => markdown divider and not to be touched while yaml parsing
	*/
	splitContents := strings.Split(filecontent, "---")
	frontmatterSplit := ""

	if len(splitContents) <= 1 {
		return Frontmatter{}, "", "", false
	}

	// If the first section of the page contains a title field, continue parsing
	// Else, prevent parsing of the current file
	// TODO: Add this to documentation
	regex := regexp.MustCompile(`title(.*): (.*)`)
	match := regex.FindStringSubmatch(splitContents[1])

	if match == nil {
		return Frontmatter{}, "", "", false
	}

	frontmatterSplit = splitContents[1]
	// Parsing YAML frontmatter
	err := yaml.Unmarshal([]byte(frontmatterSplit), &parsedFrontmatter)
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}

	if parsedFrontmatter.Layout == "" {
		parsedFrontmatter.Layout = "page"
	}

	// parent_collection := parsedFrontmatter.Collections[0]
	// child_collection := parsedFrontmatter.Collections[1]

	// sushi_collection := Sushi{child_collection: child_collection, parsent_collection: parent_collection}

	// workshop.html
	// +---> collection is workshop
	// 						+---> from DDM parent collection is homebrew
	// 															^^^^^
	// 								assgin "hombrew" to the collection map in frontmatter

	// INSIDE DDM Struct
	// -----------------------
	// Parent Collections: homebrew, hsp
	// Child Collections []Sushi -> {workshop, homebrew}, {features, hsp}
	// 					  ^^^^ not good

	// hsp-ecc.xyz/homebrew/about.md
	// 		Frontmatter: Collection: [homebrew]
	//
	// hsp-ecc.xyz/homebrew/workshops_post/zig.md
	// 		Frontmatter: Collection: [homebrew, workshop]
	//
	// hsp-ecc.xyz/hsp/events/zig.md
	// 		Frontmatter: Collection: [hsp, events]

	markdown = strings.Join(strings.Split(filecontent, "---")[2:], "---")

	// Parsing markdown to HTML
	var parsedMarkdown bytes.Buffer
	var md goldmark.Markdown

	if parsedFrontmatter.Type == "post" || parsedFrontmatter.TOC {
		md = goldmark.New(
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
			goldmark.WithExtensions(
				extension.TaskList,
				figure.Figure,
				&toc.Extender{
					Compact: true,
				},
				&mermaid.Extender{
					RenderMode: mermaid.RenderModeClient, // or RenderModeClient
				},
				&anchor.Extender{
					Texter: anchor.Text("#"),
				},
			),
			goldmark.WithRendererOptions(
				html.WithUnsafe(),
			),
		)
	} else {
		md = goldmark.New(
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
			goldmark.WithExtensions(
				extension.TaskList,
				figure.Figure,
				&mermaid.Extender{
					RenderMode: mermaid.RenderModeClient, // or RenderModeClient
				},
			),
			goldmark.WithRendererOptions(
				html.WithUnsafe(),
			),
		)
	}

	if err := md.Convert([]byte(markdown), &parsedMarkdown); err != nil {
		p.ErrorLogger.Fatal(err)
	}

	return parsedFrontmatter, parsedMarkdown.String(), markdown, true
}

func (p *Parser) DateParse(date string) time.Time {
	parsedTime, err := time.Parse("2006-01-02", date)
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}
	return parsedTime
}

func (p *Parser) ParseConfig(inFilePath string) {
	// // Check if the configuration file exists
	// _, err := os.Stat(inFilePath)
	// if os.IsNotExist(err) {
	// 	p.Helper.Bootstrap()
	// 	return
	// }

	// Read and parse the configuration file
	configFile, err := os.ReadFile(inFilePath)
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}

	err = yaml.Unmarshal(configFile, &p.LayoutConfig)
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}
}

func (p *Parser) ParseRobots(inFilePath string, outFilePath string) {
	tmpl, err := template.ParseFiles(inFilePath)
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}

	var buffer bytes.Buffer
	err = tmpl.Execute(&buffer, p.LayoutConfig)
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}

	outputFile, err := os.Create(outFilePath)
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}
	defer func() {
		err = outputFile.Close()
		if err != nil {
			p.ErrorLogger.Fatal(err)
		}
	}()

	_, err = outputFile.Write(buffer.Bytes())
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}
}

// ParseLayoutFiles Parse all the ".html" layout files in the layout/ directory
func (p *Parser) ParseLayoutFiles() *template.Template {
	// Parsing all files in the layout/ dir which match the "*.html" pattern
	templ, err := template.ParseGlob(p.SiteDataPath + "layout/*.html")
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}

	// Parsing all files in the partials/ dir which match the "*.html" pattern
	templ, err = templ.ParseGlob(p.SiteDataPath + "layout/partials/*.html")
	if err != nil {
		p.ErrorLogger.Fatal(err)
	}

	return templ
}
