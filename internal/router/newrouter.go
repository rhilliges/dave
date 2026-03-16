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
	fs        fs.FS
	resolvers map[string]ResolverFunc
	templates *template.Template
}

type render struct {
	layout   string
	template string
	data     map[string]any
}

func NewRouter(fs fs.FS) *Router {
	return &Router{fs, make(map[string]ResolverFunc), template.New("root")}
}

func (router *Router) Use(s string, resolver ResolverFunc) {
	router.resolvers[s] = resolver
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	router.templates = scanTemplates(router.fs)
	log.Println(router.templates.DefinedTemplates())

	render, _ := router.getRender(r)
	log.Println(render)
	if render.layout == "" {
		err := router.templates.ExecuteTemplate(w, render.template, render.data)
		log.Println(err)
		return
	}
	content := &strings.Builder{}
	err := router.templates.ExecuteTemplate(content, render.template, render.data)
	log.Println(err)
	err = router.templates.ExecuteTemplate(w, render.layout, map[string]string{"content": content.String()})
	log.Println(err)
}

func scanTemplates(root fs.FS) *template.Template {
	rootTemplate := template.New("root")
	fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Panic(err)
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		newTemplate := rootTemplate.New(stripTemplateSuffix(path))
		file, err := root.Open(path)
		if err != nil {
			log.Panic(err)
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			log.Panic(err)
		}
		_, err = newTemplate.Parse(string(content))
		if err != nil {
			log.Panic(err)
		}
		return nil
	})
	return rootTemplate
}

func (router *Router) getRender(r *http.Request) (*render, error) {
	render := &render{}
	templateName := r.Header.Get("D-TEMPLATE")
	if templateName == "" {
		templateName = "index"
	}
	reqPath := strings.Join([]string{r.URL.Path, templateName}, "/")
	reqSegments := strings.Split(reqPath[1:], "/")
	values := make(map[string]any)
	templatePath := ""

	for _, v := range router.templates.Templates() {
		if v.Name() == "root" {
			continue
		}
		path := stripTemplateSuffix(v.Name())
		pathSegments := strings.Split(path, "/")
		if len(pathSegments) != len(reqSegments) {
			continue
		}
		found := true
		for i, seg := range pathSegments {
			if seg == reqSegments[i] {
				continue
			} else {
				if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
					varName := seg[1 : len(seg)-1]
					resolver := router.resolvers[varName]
					log.Println(varName)
					if resolver != nil {
						values[varName] = resolver(r.Context(), reqSegments[i])
					} else {
						values[varName] = reqSegments[i]
					}
					log.Println(values[varName])
				} else {
					found = false
					break
				}
			}
		}
		if found {
			templatePath = path
		} else {
			values = make(map[string]any)
		}
	}
	if templatePath == "" {
		return nil, nil // return error (not found)
	}
	render.data = values
	render.template = templatePath
	log.Println(render.data)

	layoutPath := r.Header.Get("D-LAYOUT")
	if layoutPath == "" {
		layoutPath = "default"
	}
	layoutPath = strings.Join([]string{"layouts", layoutPath}, "/")
	layoutTemplate := router.templates.Lookup(layoutPath)
	if layoutTemplate == nil {
		log.Printf("layout not found: %s, rendering w/o layout", layoutPath)
		return render, nil
	}

	render.layout = layoutPath
	return render, nil
}

func stripTemplateSuffix(t string) string {
	i := strings.LastIndex(t, ".tmpl")
	if i < 0 {
		return t
	}
	return t[:i]
}
