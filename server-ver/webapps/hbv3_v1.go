package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	mopt "go.mongodb.org/mongo-driver/mongo/options"
)

type Link struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"        json:"id"`
	UserID       string             `bson:"userId"               json:"userId"`
	Name         string             `bson:"name"                 json:"name"`
	URL          string             `bson:"url"                  json:"url"`
	Description  string             `bson:"description,omitempty"json:"description,omitempty"`
	Tags         []string           `bson:"tags,omitempty"       json:"tags,omitempty"`
	Pinned       bool               `bson:"pinned,omitempty"     json:"pinned,omitempty"`
	Usage        int64              `bson:"usage,omitempty"      json:"usage,omitempty"`
	FaviconURL   string             `bson:"faviconUrl,omitempty" json:"faviconUrl,omitempty"`
	FaviconImage string             `bson:"faviconImage,omitempty" json:"faviconImage,omitempty"` // например base64
	Thumbnail    string             `bson:"thumbnail,omitempty"  json:"thumbnail,omitempty"`
	Note         string             `bson:"note,omitempty"       json:"note,omitempty"`
	Content      string             `bson:"content,omitempty"    json:"content,omitempty"`
	Status       int                `bson:"status,omitempty"     json:"status,omitempty"` // последний HTTP статус
	CreatedAt    time.Time          `bson:"createdAt"            json:"createdAt"`
	UpdatedAt    time.Time          `bson:"updatedAt"            json:"updatedAt"`
}

type ListResponse struct {
	Items     []Link `json:"items"`
	Total     int64  `json:"total"`
	Page      int64  `json:"page"`
	Limit     int64  `json:"limit"`
	HasNext   bool   `json:"hasNext"`
	HasPrev   bool   `json:"hasPrev"`
	ElapsedMs int64  `json:"elapsedMs"`
}

var (
	db  *mongo.Database
	col *mongo.Collection
)

func main() {
	// ENV: MONGO_URI, MONGO_DB, PORT
	uri := getenv("MONGO_URI", "mongodb+srv://resagera:SS4zZ66y60rYT2EI@cluster0.w7vun6l.mongodb.net")
	dbName := getenv("MONGO_DB", "res_serv_bot")
	port := getenv("PORT", "8676")

	cli, err := mongo.Connect(context.Background(), mopt.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	db = cli.Database(dbName)
	col = db.Collection("links")

	if err := ensureIndexes(context.Background()); err != nil {
		log.Fatalf("ensureIndexes: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/habits/health", handleHealth)

	mux.HandleFunc("POST /api/habits/links", handleCreateLink)
	mux.HandleFunc("GET /api/habits/links", handleListLinks)
	mux.HandleFunc("GET /api/habits/links/{id}", handleGetLink)
	mux.HandleFunc("PUT /api/habits/links/{id}", handleUpdateLink)
	mux.HandleFunc("DELETE /api/habits/links/{id}", handleDeleteLink)

	// Увеличить usage (переход/копирование)
	mux.HandleFunc("POST /api/habits/links/{id}/bump", handleBumpUsage)

	// Проверить ссылку и обновить status (опционально)
	mux.HandleFunc("POST /api/habits/links/{id}/check", handleCheckStatus)

	slog.Info("listening", "port", port, "mongo", uri, "db", dbName)
	log.Fatal(http.ListenAndServe(":"+port, withJSON(mux)))
}

func getenv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func ensureIndexes(ctx context.Context) error {
	// TTL не делаем; создаём индексы
	// Текстовый индекс для поиска
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "pinned", Value: 1},
				{Key: "status", Value: 1},
				{Key: "createdAt", Value: -1},
				{Key: "updatedAt", Value: -1},
			},
			Options: mopt.Index().SetName("base_fields"),
		},
		{
			Keys: bson.D{
				{Key: "tags", Value: 1},
			},
			Options: mopt.Index().SetName("tags_idx"),
		},
		{
			Keys: bson.D{
				{Key: "name", Value: "text"},
				{Key: "url", Value: "text"},
				{Key: "description", Value: "text"},
				{Key: "note", Value: "text"},
				{Key: "content", Value: "text"},
			},
			Options: mopt.Index().SetName("fulltext"),
		},
	})
	return err
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now(),
	})
}

func handleCreateLink(w http.ResponseWriter, r *http.Request) {
	var in Link
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpError(w, 400, err)
		return
	}
	if in.UserID == "" || in.URL == "" {
		httpError(w, 400, errors.New("userId and url are required"))
		return
	}
	now := time.Now().UTC()
	in.ID = primitive.NilObjectID
	in.CreatedAt = now
	in.UpdatedAt = now
	if in.Tags == nil {
		in.Tags = []string{}
	}
	res, err := col.InsertOne(r.Context(), in)
	if err != nil {
		httpError(w, 500, err)
		return
	}
	in.ID = res.InsertedID.(primitive.ObjectID)
	writeJSON(w, http.StatusCreated, in)
}

func handleGetLink(w http.ResponseWriter, r *http.Request) {
	id, ok := muxParam(r, "id")
	if !ok {
		httpError(w, 400, errors.New("missing id"))
		return
	}
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		httpError(w, 400, err)
		return
	}
	var out Link
	if err := col.FindOne(r.Context(), bson.M{"_id": objID}).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			httpError(w, 404, errors.New("not found"))
			return
		}
		httpError(w, 500, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handleUpdateLink(w http.ResponseWriter, r *http.Request) {
	id, ok := muxParam(r, "id")
	if !ok {
		httpError(w, 400, errors.New("missing id"))
		return
	}
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		httpError(w, 400, err)
		return
	}
	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpError(w, 400, err)
		return
	}
	// не даём менять _id/createdAt напрямую
	delete(in, "id")
	delete(in, "_id")
	delete(in, "createdAt")
	in["updatedAt"] = time.Now().UTC()

	upd := bson.M{"$set": in}
	opts := mopt.FindOneAndUpdate().SetReturnDocument(mopt.After)
	var out Link
	if err := col.FindOneAndUpdate(r.Context(), bson.M{"_id": objID}, upd, opts).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			httpError(w, 404, errors.New("not found"))
			return
		}
		httpError(w, 500, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	id, ok := muxParam(r, "id")
	if !ok {
		httpError(w, 400, errors.New("missing id"))
		return
	}
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		httpError(w, 400, err)
		return
	}
	res, err := col.DeleteOne(r.Context(), bson.M{"_id": objID})
	if err != nil {
		httpError(w, 500, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": res.DeletedCount})
}

func handleBumpUsage(w http.ResponseWriter, r *http.Request) {
	id, ok := muxParam(r, "id")
	if !ok {
		httpError(w, 400, errors.New("missing id"))
		return
	}
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		httpError(w, 400, err)
		return
	}
	upd := bson.M{
		"$inc": bson.M{"usage": 1},
		"$set": bson.M{"updatedAt": time.Now().UTC()},
	}
	opts := mopt.FindOneAndUpdate().SetReturnDocument(mopt.After)
	var out Link
	if err := col.FindOneAndUpdate(r.Context(), bson.M{"_id": objID}, upd, opts).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			httpError(w, 404, errors.New("not found"))
			return
		}
		httpError(w, 500, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handleCheckStatus(w http.ResponseWriter, r *http.Request) {
	// простой HEAD-запрос и запись кода
	id, ok := muxParam(r, "id")
	if !ok {
		httpError(w, 400, errors.New("missing id"))
		return
	}
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		httpError(w, 400, err)
		return
	}
	var lnk Link
	if err := col.FindOne(r.Context(), bson.M{"_id": objID}).Decode(&lnk); err != nil {
		httpError(w, 404, errors.New("not found"))
		return
	}
	client := &http.Client{Timeout: 7 * time.Second}
	req, _ := http.NewRequest("HEAD", lnk.URL, nil)
	resp, err := client.Do(req)
	code := 0
	if err == nil {
		code = resp.StatusCode
		resp.Body.Close()
	}
	upd := bson.M{"$set": bson.M{"status": code, "updatedAt": time.Now().UTC()}}
	if _, err := col.UpdateByID(r.Context(), objID, upd); err != nil {
		httpError(w, 500, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": code})
}

// /api/habits/links?q=&userId=&tags=go,docs&status=200&pinned=true&from=2025-01-01&to=2025-12-31&page=1&limit=50&sort=-createdAt
func handleListLinks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	tags := strings.TrimSpace(r.URL.Query().Get("tags"))
	statusStr := strings.TrimSpace(r.URL.Query().Get("status"))
	pinnedStr := strings.TrimSpace(r.URL.Query().Get("pinned"))
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	sortStr := strings.TrimSpace(r.URL.Query().Get("sort"))
	page := parseInt64(r.URL.Query().Get("page"), 1)
	limit := parseInt64(r.URL.Query().Get("limit"), 50)
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	skip := (page - 1) * limit

	filter := bson.M{}
	if userID != "" {
		filter["userId"] = userID
	}
	if q != "" {
		filter["$text"] = bson.M{"$search": q}
	}
	if tags != "" {
		arr := splitNonEmpty(tags, ",")
		if len(arr) > 0 {
			filter["tags"] = bson.M{"$all": arr}
		}
	}
	if statusStr != "" {
		if v, err := strconv.Atoi(statusStr); err == nil {
			filter["status"] = v
		}
	}
	if pinnedStr != "" {
		if b, err := strconv.ParseBool(pinnedStr); err == nil {
			filter["pinned"] = b
		}
	}
	// диапазоны дат по createdAt
	dateRange := bson.M{}
	if from != "" {
		if t, err := time.Parse(time.DateOnly, from); err == nil {
			dateRange["$gte"] = t
		}
	}
	if to != "" {
		if t, err := time.Parse(time.DateOnly, to); err == nil {
			// сделать до конца дня
			dateRange["$lte"] = t.Add(24 * time.Hour)
		}
	}
	if len(dateRange) > 0 {
		filter["createdAt"] = dateRange
	}

	findOpts := mopt.Find().SetSkip(skip).SetLimit(limit)
	if sortStr != "" {
		findOpts.SetSort(parseSort(sortStr))
	} else {
		findOpts.SetSort(bson.D{{Key: "createdAt", Value: -1}})
	}

	cur, err := col.Find(ctx, filter, findOpts)
	if err != nil {
		httpError(w, 500, err)
		return
	}
	defer cur.Close(ctx)

	var items []Link
	if err := cur.All(ctx, &items); err != nil {
		httpError(w, 500, err)
		return
	}

	total, err := col.CountDocuments(ctx, filter)
	if err != nil {
		httpError(w, 500, err)
		return
	}

	writeJSON(w, 200, ListResponse{
		Items:     items,
		Total:     total,
		Page:      page,
		Limit:     limit,
		HasNext:   (skip + limit) < total,
		HasPrev:   page > 1,
		ElapsedMs: time.Since(start).Milliseconds(),
	})
}

/* ---------------- utils ---------------- */

func parseSort(s string) bson.D {
	// пример: "-createdAt,usage" => [{createdAt:-1},{usage:1}]
	fields := splitNonEmpty(s, ",")
	out := bson.D{}
	for _, f := range fields {
		dir := 1
		key := f
		if strings.HasPrefix(f, "-") {
			dir = -1
			key = strings.TrimPrefix(f, "-")
		}
		out = append(out, bson.E{Key: key, Value: dir})
	}
	return out
}

func splitNonEmpty(s, sep string) []string {
	raw := strings.Split(s, sep)
	res := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v != "" {
			res = append(res, v)
		}
	}
	return res
}

func parseInt64(s string, def int64) int64 {
	if s == "" {
		return def
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return i
}

func muxParam(r *http.Request, name string) (string, bool) {
	// для net/http с pattern matching (Go 1.22+)
	v := r.PathValue(name)
	return v, v != ""
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]any{"error": err.Error()})
}
