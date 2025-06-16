package sandboxui

import (
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/assert"
)

type LoginPage struct {
	Page             playwright.Page
	LoginUsernameLoc playwright.Locator
	LoginPwLoc       playwright.Locator
	NextBtn          playwright.Locator
	LoginBtn         playwright.Locator
	Header           playwright.Locator
	Env              string
}

func NewLoginPage(page playwright.Page, environment string) *LoginPage {
	lp := &LoginPage{
		Page: page,
		Env:  environment,
	}

	if environment == "dev" {
		lp.LoginUsernameLoc = page.GetByRole("textbox", playwright.PageGetByRoleOptions{
			Name: "Red Hat login or email",
		})
		lp.LoginPwLoc = page.GetByRole("textbox", playwright.PageGetByRoleOptions{
			Name: "Password",
		})
		lp.NextBtn = page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Next",
		})
		lp.LoginBtn = page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Log in",
		})
	} else if environment == "e2e-tests" {
		lp.LoginUsernameLoc = page.GetByRole("textbox", playwright.PageGetByRoleOptions{
			Name: "Username or email",
		})
		lp.LoginPwLoc = page.GetByRole("textbox", playwright.PageGetByRoleOptions{
			Name: "Password",
		})
		lp.LoginBtn = page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Sign in",
		})
	}

	lp.Header = page.Locator("header")

	return lp
}

func (lp *LoginPage) Navigate(t *testing.T, url string) {
	_, err := lp.Page.Goto(url)
	assert.NoError(t, err)
}

func (lp *LoginPage) Login(t *testing.T, loginUsername, loginPw string) {
	err := lp.LoginUsernameLoc.Fill(loginUsername)
	assert.NoError(t, err)

	if lp.Env == "dev" {
		err := lp.NextBtn.Click()
		assert.NoError(t, err)
	}

	err = lp.LoginPwLoc.Fill(loginPw)
	assert.NoError(t, err)

	err = lp.LoginBtn.Click()
	assert.NoError(t, err)

	err = lp.Header.WaitFor()
	assert.NoError(t, err)

	text, err := lp.Header.TextContent()
	assert.NoError(t, err)
	assert.Contains(t, text, "Developer Sandbox")
}
