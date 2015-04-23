package web

import (
	"bytes"
	"container/list"
	"fmt"
	"net/http"
	"reflect"
)

// Request wraps net/http's Request and gocraf/web specific fields. In particular, PathParams is used to access
// captures params in your URL. A Request is sent to handlers on each request.
type Request struct {
	*http.Request

	// PathParams exists if you have wildcards in your URL that you need to capture.
	// Eg, /users/:id/tickets/:ticket_id and /users/1/tickets/33 would yield the map {id: "3", ticket_id: "33"}
	PathParams map[string]string

	// The actual route that got invoked
	route *Route

	rootContext   reflect.Value // Root context. Set immediately.
	targetContext reflect.Value // The target context corresponding to the route. Not set until root middleware is done.
}

// IsRouted can be called from middleware to determine if the request has been routed yet.
func (r *Request) IsRouted() bool {
	return r.route != nil
}

// RoutePath returns the routed path string. Eg, if a route was registered with
// router.Get("/suggestions/:suggestion_id/comments", f), then RoutePath will return "/suggestions/:suggestion_id/comments".
func (r *Request) RoutePath() string {
	if r.route != nil {
		return r.route.path
	}
	return ""
}

func (r *Request) UrlFor(routeName string, pathParams ...string) (string, error) {
	return r.MappedUrlFor(routeName, nil, pathParams...)
}

func (r *Request) MappedUrlFor(routeName string, namedParams map[string]string, pathParams ...string) (string, error) {
	if r.route == nil {
		return "", fmt.Errorf("Request to %s is not associated with any route.", r.RequestURI)
	}
	if namedParams == nil {
		namedParams = make(map[string]string)
	}

	discovered := list.New()
	routers := list.New()

	// Start searching from the current router,
	// but go back to the root if needed.
	routers.PushBack(r.route.router)
	didPushRootRouter := false

	for routers.Len() > 0 {
		var router *Router = routers.Remove(routers.Front()).(*Router)
		if contains(discovered, router) {
			continue
		}

		discovered.PushBack(router)

		for _, route := range router.routes {
			if route.Name == routeName {
				return fillPathParams(route.path, namedParams, pathParams...)
			}
		}

		for _, r := range router.children {
			routers.PushBack(r)
		}

		if !didPushRootRouter && routers.Len() == 0 {
			didPushRootRouter = true
			routers.PushBack(getRootRouter(r.route.router))
		}
	}

	return "", fmt.Errorf("Route with name %s was not found.", routeName)
}

func fillPathParams(path string, namedParams map[string]string, otherParams ...string) (string, error) {
	buf := new(bytes.Buffer)
	segments := splitPath(path)
	currentParam := 0
	otherParamIndex := 0
	otherParamsLength := len(otherParams)

	for _, seg := range segments {
		buf.WriteString("/")
		isWld, wldName, wldRegexpStr := isWildcard(seg)

		if isWld {
			paramVal, ok := namedParams[wldName]

			if !ok { // Try to get the param from otherParams
				if otherParamIndex >= otherParamsLength {
					return "", fmt.Errorf("Could not match parameter #%d for path '%s'.", currentParam+1, path)
				}
				paramVal = otherParams[otherParamIndex]
				otherParamIndex += 1
			}

			if wldRegexpStr != "" && !compileRegexp(wldRegexpStr).MatchString(paramVal) {
				return "", fmt.Errorf("Could not match the parameter #%d from path '%s'. Tried to match with '%s'.", currentParam+1, path, paramVal)
			}

			currentParam += 1
			buf.WriteString(paramVal)
		} else {
			buf.WriteString(seg)
		}
	}

	if otherParamIndex < otherParamsLength-1 {
		return "", fmt.Errorf("Path '%s' takes %d parameters, while %d was given.", path, otherParamIndex+1, otherParamsLength)
	}

	return buf.String(), nil
}

func contains(l *list.List, value interface{}) bool {
	for e := l.Front(); e != nil; e = e.Next() {
		if e.Value == value {
			return true
		}
	}
	return false
}

func getRootRouter(router *Router) *Router {
	for {
		if router.parent != nil {
			router = router.parent
		} else {
			break
		}
	}
	return router
}
