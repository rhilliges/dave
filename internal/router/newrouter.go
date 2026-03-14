package router

import (
	"context"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

type ResolverFunc func(ctx context.Context, value string) any

type Router struct {
	// templates *template.Template
	fs        fs.FS
	resolvers map[string]ResolverFunc
}

func NewRouter(fs fs.FS) *Router {
	return &Router{fs, make(map[string]ResolverFunc)}
}

func (router *Router) Use(s string, resolver ResolverFunc) {
	router.resolvers[s] = resolver
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	templateName := r.Header.Get("D-TEMPLATE")
	if templateName == "" {
		templateName = "index.tmpl"
	} else {
		templateName += ".tmpl"
	}
	path := strings.Join([]string{r.URL.Path, templateName}, "/")
	reqSegments := strings.Split(path[1:], "/")
	values := make(map[string]any)
	templatePath := ""
	uiTemplates := template.New("root")
	fs.WalkDir(router.fs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		newTemplate := uiTemplates.New(path)
		file, err := router.fs.Open(path)
		if err != nil {
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
		}
		log.Println(content)
		_, err = newTemplate.Parse(string(content))
		if err != nil {
		}

		pathSegments := strings.Split(path, "/")
		if len(pathSegments) != len(reqSegments) {
			return nil
		}
		for i, seg := range pathSegments {
			if seg == reqSegments[i] {
				continue
			} else {
				if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
					varName := seg[1 : len(seg)-1]
					resolver := router.resolvers[varName]
					if resolver != nil {
						values[varName] = resolver(r.Context(), reqSegments[i])
					} else {
						values[varName] = reqSegments[i]
					}
				} else {
					return nil
				}
			}
		}
		templatePath = path
		return nil
	})
	layout := r.Header.Get("D-LAYOUT")
	if layout == "" {
		layout = "default.tmpl"
	} else {
		layout += ".tmpl"
	}
	layout = strings.Join([]string{"layouts", layout}, "/")
	layoutTemplate := uiTemplates.Lookup(layout)
	if layoutTemplate == nil {
		log.Printf("layout not found: %s, rendering w/o layout", layout)
		uiTemplates.ExecuteTemplate(w, templatePath, values)
		return
	}

	log.Println(uiTemplates.DefinedTemplates())
	content := &strings.Builder{}
	// TODO error handling
	log.Println(templatePath)
	err := uiTemplates.ExecuteTemplate(content, templatePath, values)
	log.Println(err)
	err = uiTemplates.ExecuteTemplate(w, layout, map[string]string{"content": content.String()})
	log.Println(err)
}
