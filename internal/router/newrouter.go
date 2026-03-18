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

type (
	HandlerFunc func(ctx context.Context, r *http.Request) any
)
type ResolverConfFunc func(router *Router, varName string)

type ResolverFunc func(w http.ResponseWriter, r *http.Request) (any, error)

type Router struct {
	fs        fs.FS
	resolvers map[string]map[string]ResolverFunc
	templates *template.Template
}

type PathVariables map[string]string

var pathVariablesKey = new(PathVariables)

func (router *Router) UseResolver(varName string, configFunc ResolverConfFunc) {
	configFunc(router, varName)
}

func Get[K any](handler func(r *http.Request, value string) (K, error)) ResolverConfFunc {
	return func(router *Router, varName string) {
		variableResolvers := router.resolvers[varName]
		if variableResolvers == nil {
			router.resolvers[varName] = make(map[string]ResolverFunc)
		}
		router.resolvers[varName][http.MethodGet] = func(w http.ResponseWriter, r *http.Request) (any, error) {
			pathVariables := r.Context().Value(pathVariablesKey).(PathVariables)
			varValue := pathVariables[varName]
			return handler(r, varValue)
		}
	}
}

func Post[K any](handler func(w http.ResponseWriter, r *http.Request) (K, error)) ResolverConfFunc {
	return func(router *Router, varName string) {
		variableResolvers := router.resolvers[varName]
		if variableResolvers == nil {
			router.resolvers[varName] = make(map[string]ResolverFunc)
		}
		router.resolvers[varName]["POST"] = func(w http.ResponseWriter, r *http.Request) (any, error) {
			return handler(w, r)
		}
	}
}

// func Post(request, value string) (string, error func(r *http.Request, value string) (string, error)) ResolverConfFunc {
// 	panic("unimplemented")
// }

func NewRouter(fs fs.FS) *Router {
	return &Router{fs: fs, resolvers: make(map[string]map[string]ResolverFunc)}
}

type render struct {
	layout   string
	template string
	data     map[string]any
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	log.Println(err)
	// log.Println(r.PostForm.Get("input1"))
	// log.Println(r.Form.Get("input1"))
	// log.Println(r.Form)
	// log.Println(r.PostForm)
	router.templates = scanTemplates(router.fs)

	render, _ := router.getRender(w, r)
	if render.layout == "" {
		err := router.templates.ExecuteTemplate(w, render.template, render.data)
		log.Println(err)
		return
	}
	content := &strings.Builder{}
	err = router.templates.ExecuteTemplate(content, render.template, render.data)
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

func (router *Router) getRender(w http.ResponseWriter, r *http.Request) (*render, error) {
	render := &render{}
	templateName := r.Header.Get("D-TEMPLATE")
	if templateName == "" {
		templateName = "index"
	}
	reqPath := strings.Join([]string{r.URL.Path, templateName}, "/")

	data := make(map[string]any)

	templatePath, pathVariables := parseRequestPath(router.templates, reqPath)
	data["path_variables"] = pathVariables

	resolverCtx := context.WithValue(r.Context(), pathVariablesKey, pathVariables)
	resolverReq := r.WithContext(resolverCtx)

	keys := make([]string, 0, len(pathVariables))
	for k := range pathVariables {
		keys = append(keys, k)
	}

	for name := range pathVariables {
		resolvers := router.resolvers[name]
		if resolvers == nil {
			continue
		}
		var resolver ResolverFunc
		if name == keys[len(keys)-1] {
			resolver = resolvers[r.Method]
		} else {
			resolver = resolvers[http.MethodGet]
		}
		if resolver == nil {
			continue
		}

		d, _ := resolver(w, resolverReq)
		data[name] = d
	}
	if templatePath == "" {
		return nil, nil // return error (not found)
	}
	render.data = data
	render.template = templatePath

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

func parseRequestPath(templates *template.Template, path string) (string, PathVariables) {
	reqSegments := strings.Split(path[1:], "/")
	templatePath := ""
	pathVariables := PathVariables{}

	for _, v := range templates.Templates() {
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
					pathVariables[varName] = reqSegments[i]
				} else {
					found = false
					break
				}
			}
		}
		if found {
			templatePath = path
		} else {
			pathVariables = PathVariables{}
		}
	}
	return templatePath, pathVariables
}

func stripTemplateSuffix(t string) string {
	i := strings.LastIndex(t, ".tmpl")
	if i < 0 {
		return t
	}
	return t[:i]
}
