package main

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

type campArchive struct {
	UUID      string    `json:"uuid"`
	Subject   string    `json:"subject"`
	CreatedAt null.Time `json:"created_at"`
	URL       string    `json:"url"`
}

// handleGetCampaignArchives renders the public campaign archives page.
func handleGetCampaignArchives(c echo.Context) error {
	var (
		app = c.Get("app").(*App)
		pg  = getPagination(c.QueryParams(), 50)
	)

	camps, total, err := getCampaignArchives(pg.Offset, pg.Limit, app)
	if err != nil {
		return err
	}

	var out models.PageResults
	if len(camps) == 0 {
		out.Results = []campArchive{}
		return c.JSON(http.StatusOK, okResp{out})
	}

	// Meta.
	out.Results = camps
	out.Total = total
	out.Page = pg.Page
	out.PerPage = pg.PerPage

	return c.JSON(200, okResp{out})
}

// handleCampaignArchivesPage renders the public campaign archives page.
func handleCampaignArchivesPage(c echo.Context) error {
	var (
		app = c.Get("app").(*App)
		pg  = getPagination(c.QueryParams(), 50)
	)

	out, total, err := getCampaignArchives(pg.Offset, pg.Limit, app)
	if err != nil {
		return err
	}

	title := app.i18n.T("public.archiveTitle")
	return c.Render(http.StatusOK, "archive", struct {
		Title       string
		Description string
		Campaigns   []campArchive
		Total       int
		Page        int
		PerPage     int
	}{title, title, out, total, pg.Page, pg.PerPage})
}

// handleCampaignArchivePage renders the public campaign archives page.
func handleCampaignArchivePage(c echo.Context) error {
	var (
		app  = c.Get("app").(*App)
		uuid = c.Param("uuid")
	)

	pubCamp, err := app.core.GetArchivedCampaign(0, uuid)
	if err != nil {
		if er, ok := err.(*echo.HTTPError); ok {
			if er.Code == http.StatusBadRequest {
				return c.Render(http.StatusNotFound, tplMessage,
					makeMsgTpl(app.i18n.T("public.notFoundTitle"), "", app.i18n.T("public.campaignNotFound")))
			}
		}

		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(app.i18n.T("public.errorTitle"), "", app.i18n.Ts("public.errorFetchingCampaign")))
	}

	out, err := compileArchiveCampaigns([]models.Campaign{pubCamp}, app)
	if err != nil {
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(app.i18n.T("public.errorTitle"), "", app.i18n.Ts("public.errorFetchingCampaign")))
	}

	// Render the message body.
	camp := out[0].Campaign
	msg, err := app.manager.NewCampaignMessage(camp, out[0].Subscriber)
	if err != nil {
		app.log.Printf("error rendering message: %v", err)
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(app.i18n.T("public.errorTitle"), "", app.i18n.Ts("public.errorFetchingCampaign")))
	}

	return c.HTML(http.StatusOK, string(msg.Body()))
}

func getCampaignArchives(offset, limit int, app *App) ([]campArchive, int, error) {
	pubCamps, total, err := app.core.GetArchivedCampaigns(offset, limit)
	if err != nil {
		return []campArchive{}, total, echo.NewHTTPError(http.StatusInternalServerError, app.i18n.T("public.errorFetchingCampaign"))
	}

	msgs, err := compileArchiveCampaigns(pubCamps, app)
	if err != nil {
		return []campArchive{}, total, err
	}

	out := make([]campArchive, 0, len(msgs))
	for _, m := range msgs {
		camp := m.Campaign
		out = append(out, campArchive{
			UUID:      camp.UUID,
			Subject:   camp.Subject,
			CreatedAt: camp.CreatedAt,
			URL:       app.constants.ArchiveURL + "/" + camp.UUID,
		})
	}

	return out, total, nil
}

func compileArchiveCampaigns(camps []models.Campaign, app *App) ([]manager.CampaignMessage, error) {
	var (
		b = bytes.Buffer{}
	)

	out := make([]manager.CampaignMessage, 0, len(camps))
	for _, camp := range camps {
		if err := camp.CompileTemplate(app.manager.TemplateFuncs(&camp)); err != nil {
			app.log.Printf("error compiling template: %v", err)
			return nil, echo.NewHTTPError(http.StatusInternalServerError, app.i18n.T("public.errorFetchingCampaign"))
		}

		// Load the dummy subscriber meta.
		var sub models.Subscriber
		if err := json.Unmarshal([]byte(camp.ArchiveMeta), &sub); err != nil {
			app.log.Printf("error unmarshalling campaign archive meta: %v", err)
			return nil, echo.NewHTTPError(http.StatusInternalServerError, app.i18n.T("public.errorFetchingCampaign"))
		}

		m := manager.CampaignMessage{
			Campaign:   &camp,
			Subscriber: sub,
		}

		// Render the subject if it's a template.
		if camp.SubjectTpl != nil {
			if err := camp.SubjectTpl.ExecuteTemplate(&b, models.ContentTpl, m); err != nil {
				return nil, err
			}
			camp.Subject = b.String()
			b.Reset()

		}

		out = append(out, m)
	}

	return out, nil
}
