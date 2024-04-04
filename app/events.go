package app

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	matrix_db "commune/db/matrix/gen"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffail/gabs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type IndexEventsParams struct {
	Last   string `json:"last"`
	Filter string `json:"filter"`
}

func (c *App) GetIndexEvents(p *IndexEventsParams) (*[]Event, error) {

	gep := matrix_db.GetEventsParams{
		OriginServerTS: pgtype.Int8{
			Int64: time.Now().UnixMilli(),
			Valid: true,
		},
	}

	if p.Last != "" {
		i, _ := strconv.ParseInt(p.Last, 10, 64)
		log.Println(i)
		gep.OriginServerTS.Int64 = i
	}

	if p.Filter == "social" {
		gep.Social = pgtype.Bool{
			Bool:  true,
			Valid: true,
		}
	}

	if p.Filter == "spaces" {
		gep.Social = pgtype.Bool{
			Bool:  false,
			Valid: true,
		}
	}

	if c.Config.Cache.IndexEvents && p.Last == "" {

		// get events for this space from cache
		cached, err := c.Cache.Events.Get("index").Result()
		if err != nil {
			log.Println("index events not in cache")
			return nil, err
		}

		if cached != "" {
			log.Println("returning index events from cache")
			var events []Event
			err = json.Unmarshal([]byte(cached), &events)
			if err != nil {
				return nil, err
			} else {
				return &events, nil
			}
		}
	}

	events, err := c.MatrixDB.Queries.GetEvents(context.Background(), gep)

	if err != nil {
		return nil, err
	}

	items := []Event{}

	if p.Last == "" {
		pinned, err := c.Cache.System.Get("pinned").Result()

		if err == nil && pinned != "" {

			var us []string
			err = json.Unmarshal([]byte(pinned), &us)

			if err == nil {

				for _, pi := range us {

					item, err := c.GetEvent(&GetEventParams{
						Slug: pi,
					})

					if err != nil {
						log.Println(err)
					}

					if item != nil {
						item.Pinned = true
						items = append(items, *item)
					}

				}

			}
		}
	}

	for _, item := range events {

		json, err := gabs.ParseJSON([]byte(item.JSON.String))
		if err != nil {
			log.Println("error parsing json: ", err)
			return nil, err
		}

		s := ProcessComplexEvent(&EventProcessor{
			EventID:     item.EventID,
			Slug:        item.Slug,
			RoomAlias:   item.RoomAlias.String,
			JSON:        json,
			DisplayName: item.DisplayName.String,
			AvatarURL:   item.AvatarUrl.String,
			ReplyCount:  item.Replies,
			Reactions:   item.Reactions,
			Edited:      item.Edited,
			EditedOn:    item.EditedOn,
		})

		exists := false

		for _, event := range items {
			if s.EventID == event.EventID {
				exists = true
				break
			}
		}

		if !exists {
			items = append(items, s)
		}
	}

	if c.Config.Cache.IndexEvents && p.Last == "" {
		go func() {

			serialized, err := json.Marshal(items)
			if err != nil {
				log.Println(err)
			}

			err = c.Cache.Events.Set("index", serialized, 0).Err()
			if err != nil {
				log.Println(err)
			}

		}()
	}

	return &items, nil
}

func (c *App) AllEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		user := c.LoggedInUser(r)

		if user != nil {
			log.Println("user is ", user.Username)
		}

		query := r.URL.Query()
		last := query.Get("last")
		filter := query.Get("filter")

		// get events for this space

		events, err := c.GetIndexEvents(&IndexEventsParams{
			Last:   last,
			Filter: filter,
		})

		if err != nil {
			log.Println("error getting events: ", err)
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusOK,
				JSON: map[string]any{
					"error": err,
				},
			})
			return
		}

		RespondWithJSON(w, &JSONResponse{
			Code: http.StatusOK,
			JSON: map[string]any{
				"events": events,
			},
		})

	}
}

type FeedEventsParams struct {
	Last         string
	Filter       string
	MatrixUserID string
}

func (c *App) GetUserFeedEvents(p *FeedEventsParams) (*[]Event, error) {

	fe := matrix_db.GetUserFeedEventsParams{
		UserID: pgtype.Text{
			String: p.MatrixUserID,
			Valid:  true,
		},
	}

	if p.Last != "" {
		i, _ := strconv.ParseInt(p.Last, 10, 64)
		log.Println(i)
		fe.OriginServerTS = pgtype.Int8{
			Int64: i,
			Valid: true,
		}
	}

	if p.Filter == "social" {
		fe.Social = pgtype.Bool{
			Bool:  true,
			Valid: true,
		}
	}

	if p.Filter == "spaces" {
		fe.Social = pgtype.Bool{
			Bool:  false,
			Valid: true,
		}
	}

	events, err := c.MatrixDB.Queries.GetUserFeedEvents(context.Background(), fe)

	if err != nil {
		return nil, err
	}

	items := []Event{}

	for _, item := range events {

		json, err := gabs.ParseJSON([]byte(item.JSON.String))
		if err != nil {
			log.Println("error parsing json: ", err)
			return nil, err
		}

		s := ProcessComplexEvent(&EventProcessor{
			EventID:     item.EventID,
			Slug:        item.Slug,
			RoomAlias:   item.RoomAlias.String,
			JSON:        json,
			DisplayName: item.DisplayName.String,
			AvatarURL:   item.AvatarUrl.String,
			ReplyCount:  item.Replies,
			Reactions:   item.Reactions,
			Edited:      item.Edited,
			EditedOn:    item.EditedOn,
		})

		items = append(items, s)
	}

	return &items, nil
}

func (c *App) UserFeedEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		user := c.LoggedInUser(r)

		query := r.URL.Query()
		last := query.Get("last")
		filter := query.Get("filter")

		events, err := c.GetUserFeedEvents(&FeedEventsParams{
			Last:         last,
			Filter:       filter,
			MatrixUserID: user.MatrixUserID,
		})

		if err != nil {
			log.Println("error getting events: ", err)
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusOK,
				JSON: map[string]any{
					"error": err,
				},
			})
			return
		}

		if len(*events) == 0 {
		}

		RespondWithJSON(w, &JSONResponse{
			Code: http.StatusOK,
			JSON: map[string]any{
				"events": events,
			},
		})

	}
}

type GetEventParams struct {
	Slug        string
	WithReplies bool
}

func (c *App) GetEvent(p *GetEventParams) (*Event, error) {

	item, err := c.MatrixDB.Queries.GetSpaceEvent(context.Background(), p.Slug)

	if err != nil {
		log.Println("error getting event: ", err)
		return nil, err
	}

	json, err := gabs.ParseJSON([]byte(item.JSON.String))
	if err != nil {
		log.Println("error parsing json: ", err)
		return nil, err
	}

	s := ProcessComplexEvent(&EventProcessor{
		EventID:     item.EventID,
		JSON:        json,
		DisplayName: item.DisplayName.String,
		Slug:        item.Slug,

		RoomAlias:     item.RoomAlias.String,
		AvatarURL:     item.AvatarUrl.String,
		ReplyCount:    item.Replies,
		Reactions:     item.Reactions,
		Edited:        item.Edited,
		EditedOn:      item.EditedOn,
		TransactionID: item.TxnID.String,
	})

	// get event replies
	/*
		eventReplies, err := c.MatrixDB.Queries.GetSpaceEventReplies(context.Background(), item.EventID)

		if err != nil {
			log.Println("error getting event replies: ", err)
		}

		var replies []interface{}
		{

			for _, item := range eventReplies {

				json, err := gabs.ParseJSON([]byte(item.JSON.String))
				if err != nil {
					log.Println("error parsing json: ", err)
				}

				s := ProcessComplexEvent(&EventProcessor{
					EventID:     item.EventID,
					JSON:        json,
					DisplayName: item.DisplayName.String,
					RoomAlias:   item.RoomAlias.String,
					AvatarURL:   item.AvatarUrl.String,
					Reactions:   item.Reactions,
				})

				replies = append(replies, s)
			}
		}
	*/

	return &s, nil
}

func (c *App) Event() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		event := chi.URLParam(r, "event")

		item, err := c.GetEvent(&GetEventParams{
			Slug: event,
		})

		if err != nil {
			log.Println("error getting event: ", err)
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusOK,
				JSON: map[string]any{
					"error":  "event not found",
					"exists": false,
				},
			})
			return
		}

		user := c.LoggedInUser(r)

		resp := map[string]any{
			"event": item,
		}

		query := r.URL.Query()
		replies := query.Get("replies")

		if replies == "true" {
			gep := &GetEventRepliesParams{
				Slug: event,
			}

			if user != nil {
				gep.MatrixUserID = user.MatrixUserID
			}

			replies, err := c.GetEventReplies(gep)
			if err == nil && replies != nil {
				resp["replies"] = replies
			}
		}

		RespondWithJSON(w, &JSONResponse{
			Code: http.StatusOK,
			JSON: resp,
		})

	}
}

type GetEventRepliesParams struct {
	Slug         string
	MatrixUserID string
}

func (c *App) GetEventReplies(p *GetEventRepliesParams) (*[]*Event, error) {

	if c.Config.Cache.EventReplies {

		// get events for this space from cache
		cached, err := c.Cache.Events.Get(p.Slug).Result()
		if err != nil {
			log.Println("event replies for %s not in cache", p.Slug)
		}

		if cached != "" {
			var events []*Event
			err = json.Unmarshal([]byte(cached), &events)
			if err != nil {
				log.Println(err)
				return nil, err
			} else {
				log.Println("responding with cached event replies")
				return &events, nil
			}
		}
	}

	gsp := matrix_db.GetSpaceEventRepliesParams{
		Slug: pgtype.Text{String: p.Slug, Valid: true},
	}

	if p.MatrixUserID != "" {
		gsp.Sender = pgtype.Text{String: p.MatrixUserID, Valid: true}
	}

	replies, err := c.MatrixDB.Queries.GetSpaceEventReplies(context.Background(), gsp)

	if err != nil {
		log.Println("error getting event replies: ", err)
		return nil, err
	}

	var items []*Event

	for _, item := range replies {

		json, err := gabs.ParseJSON([]byte(item.JSON.String))
		if err != nil {
			log.Println("error parsing json: ", err)
		}

		s := ProcessComplexEvent(&EventProcessor{
			EventID:     item.EventID,
			Slug:        item.Slug,
			JSON:        json,
			DisplayName: item.DisplayName.String,
			RoomAlias:   item.RoomAlias.String,
			AvatarURL:   item.AvatarUrl.String,
			Reactions:   item.Reactions,
			Edited:      item.Edited,
			EditedOn:    item.EditedOn,
		})

		s.Upvotes = item.Upvotes.Int64
		s.Downvotes = item.Downvotes.Int64

		s.Upvoted = item.Upvoted
		s.Downvoted = item.Downvoted

		s.InReplyTo = item.InReplyTo

		items = append(items, &s)
	}

	sorted := SortEvents(items)

	go func() {
		if c.Config.Cache.EventReplies {

			serialized, err := json.Marshal(sorted)
			if err != nil {
				log.Println(err)
			}

			err = c.Cache.Events.Set(p.Slug, serialized, 0).Err()
			if err != nil {
				log.Println(err)
			}
		}

	}()

	return &sorted, nil
}

func (c *App) EventReplies() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		event := chi.URLParam(r, "event")

		replies, err := c.GetEventReplies(&GetEventRepliesParams{
			Slug: event,
		})

		if err != nil {
			log.Println("error getting event replies: ", err)
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusOK,
				JSON: map[string]any{
					"error":  "couldn't get event replies",
					"exists": false,
				},
			})
			return
		}

		RespondWithJSON(w, &JSONResponse{
			Code: http.StatusOK,
			JSON: map[string]any{
				"replies": replies,
			},
		})

	}
}

func (c *App) GetPinnedEvents(roomID string) ([]Event, error) {

	events, err := c.MatrixDB.Queries.GetPinnedEvents(context.Background(), roomID)

	if err != nil {
		log.Println("error getting event: ", err)
		return nil, err
	}

	pinned := []string{}

	if bytes, ok := events.(string); ok {
		err := json.Unmarshal([]byte(bytes), &pinned)
		if err != nil {
			log.Println("Failed to unmarshal JSON:", err)
			return nil, err
		}
	}

	items := []Event{}

	if len(pinned) > 0 {
		for _, event := range pinned {
			slug := event[len(event)-11:]
			item, err := c.GetEvent(&GetEventParams{
				Slug: slug,
			})
			if err == nil && item != nil {
				item.Pinned = true
				items = append(items, *item)
			}
		}
	}

	return items, nil
}

type SpaceEventsParams struct {
	RoomID string
	Last   string
	After  string
	Topic  string
}

func (c *App) GetSpaceEvents(p *SpaceEventsParams) (*[]Event, error) {

	sreq := matrix_db.GetSpaceEventsParams{
		RoomID: p.RoomID,
	}

	if len(p.Topic) > 0 {
		sreq.Topic = pgtype.Text{
			String: p.Topic,
			Valid:  true,
		}
	}

	// get events for this space

	if p.Last != "" {
		i, _ := strconv.ParseInt(p.Last, 10, 64)
		log.Println("adding last", i)
		sreq.OriginServerTS = pgtype.Int8{
			Int64: i,
			Valid: true,
		}
	}

	if p.After != "" {
		i, _ := strconv.ParseInt(p.After, 10, 64)
		log.Println(i)
	}

	if c.Config.Cache.SpaceEvents && p.Last == "" {

		// get events for this space from cache
		cached, err := c.Cache.Events.Get(p.RoomID).Result()
		if err != nil {
			log.Println("index events not in cache")
		}

		if cached != "" {
			var events []Event
			err = json.Unmarshal([]byte(cached), &events)
			if err != nil {
				log.Println(err)
			} else {
				log.Println("responding with cached events")
				return &events, nil
			}
		}
	}

	items := []Event{}

	if p.Last == "" && len(p.Topic) == 0 {
		pinned, err := c.GetPinnedEvents(p.RoomID)
		if err != nil {
			log.Println("error getting pinned events: ", err)
		}
		for _, item := range pinned {
			items = append(items, item)
		}
	}

	// get events for this space
	events, err := c.MatrixDB.Queries.GetSpaceEvents(context.Background(), sreq)

	if err != nil {
		log.Println("error getting event: ", err)
		return nil, err
	}

	for _, item := range events {

		json, err := gabs.ParseJSON([]byte(item.JSON.String))
		if err != nil {
			log.Println("error parsing json: ", err)
		}

		s := ProcessComplexEvent(&EventProcessor{
			EventID:     item.EventID,
			Slug:        item.Slug,
			JSON:        json,
			RoomAlias:   item.RoomAlias.String,
			DisplayName: item.DisplayName.String,
			AvatarURL:   item.AvatarUrl.String,
			ReplyCount:  item.Replies,
			Reactions:   item.Reactions,
			Edited:      item.Edited,
			EditedOn:    item.EditedOn,
			//Reference:   item.Reference,
		})

		exists := false

		for _, event := range items {
			if s.EventID == event.EventID {
				exists = true
				break
			}
		}

		if !exists {
			items = append(items, s)
		}
	}

	if c.Config.Cache.SpaceEvents && p.Last == "" {
		go func() {

			serialized, err := json.Marshal(items)
			if err != nil {
				log.Println(err)
			}

			err = c.Cache.Events.Set(p.RoomID, serialized, 0).Err()
			if err != nil {
				log.Println(err)
			}

		}()
	}

	return &items, nil
}

func (c *App) SpaceEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		user := c.LoggedInUser(r)

		space := chi.URLParam(r, "space")

		alias := c.ConstructMatrixRoomID(space)

		alias = strings.ToLower(alias)

		isUser := strings.HasPrefix(space, "@")
		username := strings.TrimPrefix(space, "@")

		if isUser {

			deleted, err := c.MatrixDB.Queries.IsDeactivated(context.Background(), pgtype.Text{
				String: username,
				Valid:  true,
			})
			if err != nil || deleted {
				RespondWithJSON(w, &JSONResponse{
					Code: http.StatusOK,
					JSON: map[string]any{
						"error":  "space does not exist",
						"exists": false,
					},
				})
				return
			}
		}

		ssp := matrix_db.GetSpaceStateParams{
			RoomAlias: alias,
		}

		if user != nil && user.MatrixUserID != "" {
			ssp.UserID = pgtype.Text{
				String: user.MatrixUserID,
				Valid:  true,
			}
		}

		// check if space exists in DB
		state, err := c.MatrixDB.Queries.GetSpaceState(context.Background(), ssp)

		if err != nil {
			log.Println("error getting event: ", err)
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusOK,
				JSON: map[string]any{
					"error":  "space does not exist",
					"exists": false,
				},
			})
			return
		}

		hideRoom := !state.IsPublic.Bool && !state.Joined

		if hideRoom {
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusOK,
				JSON: map[string]any{
					"error":  "space does not exist",
					"exists": false,
				},
			})
			return
		}

		// get events for this space
		events, err := c.GetSpaceEvents(&SpaceEventsParams{
			RoomID: state.RoomID,
			Last:   r.URL.Query().Get("last"),
			After:  r.URL.Query().Get("after"),
			Topic:  r.URL.Query().Get("topic"),
		})

		if err != nil {
			log.Println("error getting event: ", err)
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusInternalServerError,
				JSON: map[string]any{
					"error": err,
				},
			})
			return
		}

		RespondWithJSON(w, &JSONResponse{
			Code: http.StatusOK,
			JSON: map[string]any{
				"events": events,
			},
		})

	}
}

func (c *App) SpaceRoomEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		user := c.LoggedInUser(r)

		space := chi.URLParam(r, "space")
		space = strings.ToLower(space)
		room := chi.URLParam(r, "room")
		room = strings.ToLower(room)

		log.Println("space is", space)
		log.Println("room is", room)

		alias := c.ConstructMatrixRoomID(space)

		ssp := matrix_db.GetSpaceStateParams{
			RoomAlias: alias,
		}

		if user != nil && user.MatrixUserID != "" {
			ssp.UserID = pgtype.Text{
				String: user.MatrixUserID,
				Valid:  true,
			}
		}

		// check if space exists in DB
		state, err := c.MatrixDB.Queries.GetSpaceState(context.Background(), ssp)

		if err != nil {
			log.Println("error getting event: ", err)
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusOK,
				JSON: map[string]any{
					"error":  "space does not exist",
					"exists": false,
				},
			})
			return
		}

		sps := ProcessState(state)

		scp := matrix_db.GetSpaceChildParams{
			ParentRoomAlias: pgtype.Text{
				String: alias,
				Valid:  true,
			},
			ChildRoomAlias: pgtype.Text{
				String: room,
				Valid:  true,
			},
		}

		if user != nil {
			log.Println("user is ", user.MatrixUserID)
			scp.UserID = pgtype.Text{
				String: user.MatrixUserID,
				Valid:  true,
			}
		}

		crs, err := c.MatrixDB.Queries.GetSpaceChild(context.Background(), scp)

		if err != nil || crs.ChildRoomID.String == "" {
			log.Println("error getting event: ", err)
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusOK,
				JSON: map[string]any{
					"error":  "space room does not exist",
					"state":  sps,
					"exists": false,
				},
			})
			return
		}
		log.Println("what is child room ID?", crs.ChildRoomID)

		// get events for this space
		events, err := c.GetSpaceEvents(&SpaceEventsParams{
			RoomID: crs.ChildRoomID.String,
			Last:   r.URL.Query().Get("last"),
			After:  r.URL.Query().Get("after"),
			Topic:  r.URL.Query().Get("topic"),
		})

		if err != nil {
			log.Println("error getting event: ", err)
			RespondWithJSON(w, &JSONResponse{
				Code: http.StatusInternalServerError,
				JSON: map[string]any{
					"error": err,
				},
			})
			return
		}

		// get events for this space

		RespondWithJSON(w, &JSONResponse{
			Code: http.StatusOK,
			JSON: map[string]any{
				"events": events,
			},
		})

	}
}
