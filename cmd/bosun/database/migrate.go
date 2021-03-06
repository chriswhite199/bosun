package database

import (
	"encoding/json"

	"bosun.org/models"
	"bosun.org/slog"
	"github.com/garyburd/redigo/redis"
)

// Version 0 is the schema that was never verisoned
// Version 1 migrates rendered templates from

var schemaKey = "schemaVersion"

type Migration struct {
	UID     string
	Task    func(d *dataAccess) error
	Version int64
}

var tasks = []Migration{
	{
		UID:     "Migrate Rendered Templates",
		Task:    migrateRenderedTemplates,
		Version: 1,
	},
}

type oldIncidentState struct {
	*models.IncidentState
	*models.RenderedTemplates
}

func migrateRenderedTemplates(d *dataAccess) error {
	slog.Infoln("Running rendered template migration. This can take several minutes.")

	// Hacky Work better?
	ids, err := d.getAllIncidentIdsByKeys()
	slog.Infof("migrating %v incidents", len(ids))
	if err != nil {
		return err
	}

	conn := d.Get()
	defer conn.Close()

	for _, id := range ids {
		b, err := redis.Bytes(conn.Do("GET", incidentStateKey(id)))
		if err != nil {
			return slog.Wrap(err)
		}
		oldState := &oldIncidentState{}
		if err := json.Unmarshal(b, oldState); err != nil {
			slog.Wrap(err)
		}

		incidentStateJSON, err := json.Marshal(oldState.IncidentState)
		if err != nil {
			return slog.Wrap(err)
		}
		if _, err := conn.Do("SET", incidentStateKey(oldState.Id), incidentStateJSON); err != nil {
			return slog.Wrap(err)
		}

		renderedTemplatesJSON, err := json.Marshal(oldState.RenderedTemplates)
		if err != nil {
			return slog.Wrap(err)
		}
		if _, err := conn.Do("SET", renderedTemplatesKey(oldState.Id), renderedTemplatesJSON); err != nil {
			return slog.Wrap(err)
		}

	}
	return nil
}

func (d *dataAccess) Migrate() error {
	slog.Infoln("checking migrations")
	conn := d.Get()
	defer conn.Close()

	// Since we didn't record a schema version from the start
	// we have to do some assumptions to see if this is a new
	// database, or if was a database before we started recording
	// a schema version number

	version, err := redis.Int64(conn.Do("GET", schemaKey))
	if err != nil {
		if err == redis.ErrNil {
			slog.Infoln("schema version not found in db")
			if _, err := redis.Bool(conn.Do("Get", "allIncidents")); err == redis.ErrNil {
				slog.Infoln("assuming new installation because allIncidents key not found")
				slog.Infoln("writing current schema version")
				if _, err := conn.Do("SET", schemaKey, SchemaVersion); err != nil {
					return slog.Wrap(err)
				}
				version = SchemaVersion
				return nil
			}
		} else {
			return slog.Wrap(err)
		}
	}

	for _, task := range tasks {
		if task.Version > version {
			// Check if migration has been run if not that run
			err := task.Task(d)
			if err != nil {
				return slog.Wrap(err)
			}
			if _, err := conn.Do("SET", schemaKey, task.Version); err != nil {
				return slog.Wrap(err)
			}
		}

	}
	return nil
}
