package main

import (
	handler "UKIWcoursework/Server/Handler"
	signing "UKIWcoursework/Server/Signing"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

type Pages struct {
	db            *sql.DB
	template_path string
}

func (p *Pages) executeTemplates(w http.ResponseWriter, template_name string, data interface{}) error {
	document, err := template.ParseFiles(p.template_path+"base.html", p.template_path+template_name)
	if err != nil {
		return err
	}

	err = document.Execute(w, data)
	if err != nil {
		return err
	}

	return nil
}

type DefaultTemplateData struct {
	User_details *handler.UserDetails
}

func (p *Pages) home(w http.ResponseWriter, r *http.Request, user_details *handler.UserDetails) handler.ErrorResponse {
	fmt.Println("Called home")

	if r.URL.Path != "/" {
		return handler.HTTPerror{Code: 404, Err: nil}
	}

	err := p.executeTemplates(w, "home.html", DefaultTemplateData{user_details})
	if err != nil {
		return handler.HTTPerror{Code: 500, Err: err}
	}

	return nil
}

func loginUser(w http.ResponseWriter, username string) error {
	//30 minute expiration time
	expiration := time.Now().Unix() + 1800

	payload := map[string]string{
		"username":   username,
		"expiration": strconv.Itoa(int(expiration)),
	}

	json_payload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	signature, public_key, err := signing.GenerateSignature(string(json_payload))
	if err != nil {
		return err
	}

	token := handler.Token{
		Username:   username,
		Expiration: payload["expiration"],
		Signature:  signature,
		Public_key: public_key,
	}

	json_token, err := json.Marshal(token)
	if err != nil {
		return err
	}

	cookie := new(http.Cookie)
	cookie.Name = "auth_token"
	cookie.Value = url.PathEscape(string(json_token))

	http.SetCookie(w, cookie)
	return nil
}

type LoginTemplateData struct {
	User_details  *handler.UserDetails
	Error         bool
	Error_message string
}

//obviously for testing only
func (p *Pages) login(w http.ResponseWriter, r *http.Request, user_details *handler.UserDetails) handler.ErrorResponse {
	fmt.Println("Called login")

	if r.Method == "POST" {
		stmt, err := p.db.Prepare("SELECT Password FROM UserData WHERE Username = ?")
		if err != nil {
			return handler.HTTPerror{Code: 500, Err: err}
		}

		err = r.ParseForm()
		if err != nil {
			return handler.HTTPerror{Code: 500, Err: err}
		}

		username := r.PostForm["username"][0]
		raw_password := r.PostForm["password"][0]
		database_hash := new(string)

		err = stmt.QueryRow(username).Scan(database_hash)
		if err != nil {
			data := LoginTemplateData{
				user_details,
				true,
				"The username you entered does not exist!",
			}

			err := p.executeTemplates(w, "login.html", data)
			if err != nil {
				return handler.HTTPerror{Code: 500, Err: err}
			}
			return nil
		}

		err = bcrypt.CompareHashAndPassword([]byte(*database_hash), []byte(raw_password))
		if err == nil {
			fmt.Println("Authenticated")

			err = loginUser(w, username)
			if err != nil {
				return handler.HTTPerror{Code: 500, Err: err}
			}

			r.ParseForm()
			redirect_url := r.Form.Get("return")

			if redirect_url == "" {
				redirect_url = "/"
			}

			http.Redirect(w, r, redirect_url, http.StatusSeeOther)
			return nil
		}

		data := LoginTemplateData{
			user_details,
			true,
			"The password you entered was invalid!",
		}
		err = p.executeTemplates(w, "login.html", data)
		if err != nil {
			return handler.HTTPerror{Code: 500, Err: err}
		}
		return nil
	}

	data := LoginTemplateData{
		user_details,
		false,
		"",
	}

	err := p.executeTemplates(w, "login.html", data)
	if err != nil {
		return handler.HTTPerror{Code: 500, Err: err}
	}
	return nil
}

func (p *Pages) signup(w http.ResponseWriter, r *http.Request, user_details *handler.UserDetails) handler.ErrorResponse {
	fmt.Println("Called signup")

	if r.Method == "POST" {
		stmt, err := p.db.Prepare("INSERT INTO UserData (Username, Password, Email, DOB, FirstName, LastName) VALUES (?, ?, ?, ?, ?, ?)")
		if err != nil {
			return handler.HTTPerror{Code: 500, Err: err}
		}

		err = r.ParseForm()
		if err != nil {
			return handler.HTTPerror{Code: 500, Err: err}
		}

		DOB := r.PostForm["dob-year"][0] + "-" + r.PostForm["dob-month"][0] + "-" + r.PostForm["dob-day"][0]
		password_hash, err := bcrypt.GenerateFromPassword([]byte(r.PostForm["password"][0]), 12)
		if err != nil {
			return handler.HTTPerror{Code: 500, Err: err}
		}

		_, err = stmt.Exec(
			r.PostForm["username"][0],
			string(password_hash),
			r.PostForm["email"][0],
			DOB,
			r.PostForm["firstname"][0],
			r.PostForm["lastname"][0],
		)
		if err != nil {
			return handler.HTTPerror{Code: 500, Err: err}
		}

		defer stmt.Close()

		loginUser(w, r.PostForm["username"][0])
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
	}

	err := p.executeTemplates(w, "signup.html", DefaultTemplateData{user_details})
	if err != nil {
		return handler.HTTPerror{Code: 500, Err: err}
	}

	return nil
}

func (p *Pages) myaccount(w http.ResponseWriter, r *http.Request, user_details *handler.UserDetails) handler.ErrorResponse {
	err := p.executeTemplates(w, "myaccount.html", DefaultTemplateData{user_details})
	if err != nil {
		return handler.HTTPerror{Code: 500, Err: err}
	}

	return nil
}

func (p *Pages) logout(w http.ResponseWriter, r *http.Request, user_details *handler.UserDetails) handler.ErrorResponse {
	cookie, _ := r.Cookie("auth_token")
	token, _ := handler.ParseToken(cookie)
	payload, _ := handler.GenerateSignatureToken(token)

	signing.BlacklistSignature(string(payload), token.Signature, token.Public_key)

	cookie = new(http.Cookie)
	cookie.Name = "auth_token"
	cookie.Value = "null"

	http.SetCookie(w, cookie)

	http.Redirect(w, r, "/", http.StatusSeeOther)
	return nil
}

func main() {
	file, err := os.OpenFile("logs.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		panic(err)
	}

	writer := io.MultiWriter(file, os.Stdout)
	log.SetOutput(writer)

	pages := new(Pages)
	pages.db, err = sql.Open("mysql", "matthew:MysqlPassword111@tcp(127.0.0.1:3306)/UKIW")
	if err != nil {
		panic(err)
	}

	pages.template_path = "templates/"

	//testng only
	//fs := http.FileServer(http.Dir("/home/matthew/go/src/UKIWcoursework/static"))
	//http.Handle("/static/", http.StripPrefix("/static", fs))

	http.Handle("/", handler.Handler{Middleware: pages.home, Require_login: false})
	http.Handle("/signup", handler.Handler{Middleware: pages.signup, Require_login: false})
	http.Handle("/login", handler.Handler{Middleware: pages.login, Require_login: false})
	http.Handle("/myaccount", handler.Handler{Middleware: pages.myaccount, Require_login: true})
	http.Handle("/logout", handler.Handler{Middleware: pages.logout, Require_login: true})

	fmt.Println("Server Started!")
	http.ListenAndServe("127.0.0.1:8000", nil)

}
