package beyond

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/oauth2"
)

func beyond(w http.ResponseWriter, r *http.Request) {
	setCacheControl(w)

	if r.FormValue("error") != "" {
		errorQuery(w, r)
		return
	}

	switch r.URL.Path {

	case "/launch":
		session, err := store.Get(r, *cookieName)
		if err != nil {
			session = store.New(*cookieName)
		}
		session.Values["next"] = r.FormValue("next")
		state, _ := randhex32()
		session.Values["state"] = state
		session.Save(w)

		next := oidcConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
		jsRedirect(w, next)

	case "/oidc":
		session, err := store.Get(r, *cookieName)
		if err != nil {
			errorHandler(w, 400, err.Error())
			return
		}
		if state, ok := session.Values["state"].(string); !ok || state != r.FormValue("state") {
			errorHandler(w, 403, "Invalid Browser State")
			return
		}
		email, err := oidcVerify(r.FormValue("code"))
		if err != nil {
			errorHandler(w, 401, err.Error())
			return
		}
		session.Values["user"] = email
		next, _ := session.Values["next"].(string)
		session.Values["next"] = ""
		session.Values["state"] = ""
		session.Save(w)

		http.Redirect(w, r, next, 302)

	}
}

func Handler(w http.ResponseWriter, r *http.Request) {
	// respond to loadbalancer pings
	if r.URL.Path == *healthPath {
		fmt.Fprint(w, *healthReply)
		return
	}

	// handle new logins
	if r.Host == *host {
		beyond(w, r)
		return
	} else if dockerHandler(w, r) {
		// Docker Registry v2 APIs
		return
	}

	// check for cookie authentication
	session, err := store.Get(r, *cookieName)
	if err != nil {
		session = store.New(*cookieName)
	}
	user, _ := session.Values["user"].(string)

	// check for oauth2 token
	if user == "" {
		user = tokenAuth(r)
	}
	if user != "" {
		r.Header.Set(*headerPrefix+"-User", user)
	}

	// apply whitelist
	if whitelisted(r) {
		nexthop(w, r)
		return
	}

	// force login
	if user == "" {
		login(w, r)
		return
	}

	// apply fence
	if deny(r, user) {
		errorHandler(w, 403, "Access Denied")
		return
	}

	// allow
	nexthop(w, r)
}

func login(w http.ResponseWriter, r *http.Request) {
	setCacheControl(w)
	w.WriteHeader(*fouroOneCode)

	// short-circuit WS+AJAX
	if r.Header.Get("Upgrade") != "" || r.Header.Get("X-Requested-With") != "" {
		return
	}

	jsRedirect(w, "https://"+*host+"/launch?next="+url.QueryEscape("https://"+r.Host+r.RequestURI))
}

func jsRedirect(w http.ResponseWriter, next string) {
	// hack to guarantee interactive session
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
<script type="text/javascript">
window.location.replace("%s");
</script>
`, next)
}

func setCacheControl(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
}

func randhex32() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return fmt.Sprintf("%x", b), err
}
