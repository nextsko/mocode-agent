package app

import (
	"context"

	"github.com/nextsko/mocode-agent/internal/domain/session"
)

func (app *App) useStoreServices() error {
	app.Sessions = newStoreSessionService(app.store)
	app.Messages = newStoreMessageService(app.store)
	app.History = newStoreHistoryService(app.store)
	app.FileTracker = newStoreFileTrackerService(app.store)
	if app.eventsCtx != nil {
		app.subscribeSessionServices(app.eventsCtx)
	}
	return nil
}

func (app *App) useSessionStore(_ context.Context, sess session.Session) error {
	app.sessionStoreID = sess.ID
	return nil
}

func (app *App) CreateSession(ctx context.Context, title string) (session.Session, error) {
	sess, err := app.databaseSessionService().Create(ctx, title)
	if err != nil {
		return session.Session{}, err
	}
	if err := app.useSessionStore(ctx, sess); err != nil {
		return session.Session{}, err
	}
	return sess, nil
}

func (app *App) GetSession(ctx context.Context, id string) (session.Session, error) {
	sess, err := app.databaseSessionService().Get(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	if err := app.useSessionStore(ctx, sess); err != nil {
		return session.Session{}, err
	}
	return sess, nil
}

func (app *App) ListSessions(ctx context.Context) ([]session.Session, error) {
	return app.databaseSessionService().List(ctx)
}

func (app *App) SaveSession(ctx context.Context, sess session.Session) (session.Session, error) {
	return app.databaseSessionService().Save(ctx, sess)
}

func (app *App) DeleteSession(ctx context.Context, id string) error {
	return app.databaseSessionService().Delete(ctx, id)
}

func (app *App) databaseSessionService() session.Service {
	return app.Sessions
}
