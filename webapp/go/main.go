package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-redis/redis/v8"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/labstack/gommon/log"

	_ "github.com/go-sql-driver/mysql"
)

const Limit = 20
const NazotteLimit = 50

var db *sqlx.DB
var mySQLConnectionData *MySQLConnectionEnv
var chairSearchCondition ChairSearchCondition
var estateSearchCondition EstateSearchCondition

var rdb *redis.Client

type InitializeResponse struct {
	Language string `json:"language"`
}

type Chair struct {
	ID          int64  `db:"id" json:"id"`
	Name        string `db:"name" json:"name"`
	Description string `db:"description" json:"description"`
	Thumbnail   string `db:"thumbnail" json:"thumbnail"`
	Price       int64  `db:"price" json:"price"`
	Height      int64  `db:"height" json:"height"`
	Width       int64  `db:"width" json:"width"`
	Depth       int64  `db:"depth" json:"depth"`
	Color       string `db:"color" json:"color"`
	Features    string `db:"features" json:"features"`
	Kind        string `db:"kind" json:"kind"`
	Popularity  int64  `db:"popularity" json:"-"`
	Stock       int64  `db:"stock" json:"-"`
}

type ChairSearchResponse struct {
	Count  int64   `json:"count"`
	Chairs []Chair `json:"chairs"`
}

type ChairListResponse struct {
	Chairs []Chair `json:"chairs"`
}

//Estate 物件
type Estate struct {
	ID          int64   `db:"id" json:"id"`
	Thumbnail   string  `db:"thumbnail" json:"thumbnail"`
	Name        string  `db:"name" json:"name"`
	Description string  `db:"description" json:"description"`
	Latitude    float64 `db:"latitude" json:"latitude"`
	Longitude   float64 `db:"longitude" json:"longitude"`
	Address     string  `db:"address" json:"address"`
	Rent        int64   `db:"rent" json:"rent"`
	DoorHeight  int64   `db:"door_height" json:"doorHeight"`
	DoorWidth   int64   `db:"door_width" json:"doorWidth"`
	Features    string  `db:"features" json:"features"`
	Popularity  int64   `db:"popularity" json:"-"`
}

//EstateSearchResponse estate/searchへのレスポンスの形式
type EstateSearchResponse struct {
	Count   int64    `json:"count"`
	Estates []Estate `json:"estates"`
}

type EstateListResponse struct {
	Estates []Estate `json:"estates"`
}

type Coordinate struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Coordinates struct {
	Coordinates []Coordinate `json:"coordinates"`
}

type Range struct {
	ID  int64 `json:"id"`
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

type RangeCondition struct {
	Prefix string   `json:"prefix"`
	Suffix string   `json:"suffix"`
	Ranges []*Range `json:"ranges"`
}

type ListCondition struct {
	List []string `json:"list"`
}

type EstateSearchCondition struct {
	DoorWidth  RangeCondition `json:"doorWidth"`
	DoorHeight RangeCondition `json:"doorHeight"`
	Rent       RangeCondition `json:"rent"`
	Feature    ListCondition  `json:"feature"`
}

type ChairSearchCondition struct {
	Width   RangeCondition `json:"width"`
	Height  RangeCondition `json:"height"`
	Depth   RangeCondition `json:"depth"`
	Price   RangeCondition `json:"price"`
	Color   ListCondition  `json:"color"`
	Feature ListCondition  `json:"feature"`
	Kind    ListCondition  `json:"kind"`
}

type BoundingBox struct {
	// TopLeftCorner 緯度経度が共に最小値になるような点の情報を持っている
	TopLeftCorner Coordinate
	// BottomRightCorner 緯度経度が共に最大値になるような点の情報を持っている
	BottomRightCorner Coordinate
}

type MySQLConnectionEnv struct {
	Host     string
	Port     string
	User     string
	DBName   string
	Password string
}

type RecordMapper struct {
	Record []string

	offset int
	err    error
}

func (r *RecordMapper) next() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if r.offset >= len(r.Record) {
		r.err = fmt.Errorf("too many read")
		return "", r.err
	}
	s := r.Record[r.offset]
	r.offset++
	return s, nil
}

func (r *RecordMapper) NextInt() int {
	s, err := r.next()
	if err != nil {
		return 0
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		r.err = err
		return 0
	}
	return i
}

func (r *RecordMapper) NextFloat() float64 {
	s, err := r.next()
	if err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		r.err = err
		return 0
	}
	return f
}

func (r *RecordMapper) NextString() string {
	s, err := r.next()
	if err != nil {
		return ""
	}
	return s
}

func (r *RecordMapper) Err() error {
	return r.err
}

func NewMySQLConnectionEnv() *MySQLConnectionEnv {
	return &MySQLConnectionEnv{
		Host:     getEnv("MYSQL_HOST", "127.0.0.1"),
		Port:     getEnv("MYSQL_PORT", "3306"),
		User:     getEnv("MYSQL_USER", "isucon"),
		DBName:   getEnv("MYSQL_DBNAME", "isuumo"),
		Password: getEnv("MYSQL_PASS", "isucon"),
	}
}

func getEnv(key, defaultValue string) string {
	val := os.Getenv(key)
	if val != "" {
		return val
	}
	return defaultValue
}

//ConnectDB isuumoデータベースに接続する
func (mc *MySQLConnectionEnv) ConnectDB() (*sqlx.DB, error) {
	dsn := fmt.Sprintf("%v:%v@tcp(%v:%v)/%v", mc.User, mc.Password, mc.Host, mc.Port, mc.DBName)
	return sqlx.Open("mysql", dsn)
}

func init() {
	jsonText, err := ioutil.ReadFile("../fixture/chair_condition.json")
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
	json.Unmarshal(jsonText, &chairSearchCondition)

	jsonText, err = ioutil.ReadFile("../fixture/estate_condition.json")
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
	json.Unmarshal(jsonText, &estateSearchCondition)
}

func main() {
	// redis
	rdb = redis.NewClient(&redis.Options{
		Addr: getEnv("REDIS_DSN", "localhost:6379"),
	})

	// Echo instance
	e := echo.New()
	e.Debug = true
	e.Logger.SetLevel(log.DEBUG)

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Initialize
	e.POST("/initialize", initialize)

	// Chair Handler
	e.GET("/api/chair/:id", getChairDetail)
	e.POST("/api/chair", postChair)
	e.GET("/api/chair/search", searchChairs)
	e.GET("/api/chair/low_priced", getLowPricedChair)
	e.GET("/api/chair/search/condition", getChairSearchCondition)
	e.POST("/api/chair/buy/:id", buyChair)

	// Estate Handler
	e.GET("/api/estate/:id", getEstateDetail)
	e.POST("/api/estate", postEstate)
	e.GET("/api/estate/search", searchEstates)
	e.GET("/api/estate/low_priced", getLowPricedEstate)
	e.POST("/api/estate/req_doc/:id", postEstateRequestDocument)
	e.POST("/api/estate/nazotte", searchEstateNazotte)
	e.GET("/api/estate/search/condition", getEstateSearchCondition)
	e.GET("/api/recommended_estate/:id", searchRecommendedEstateWithChair)

	mySQLConnectionData = NewMySQLConnectionEnv()

	var err error
	db, err = mySQLConnectionData.ConnectDB()
	if err != nil {
		e.Logger.Fatalf("DB connection failed : %v", err)
	}
	db.SetMaxOpenConns(10)
	defer db.Close()

	// Start server
	serverPort := fmt.Sprintf(":%v", getEnv("SERVER_PORT", "1323"))
	e.Logger.Fatal(e.Start(serverPort))
}

func initialize(c echo.Context) error {
	// これから db の中身が変わるので redis の cache も吹き飛ばす
	_ = purgeEstateIDsFromRedis()

	sqlDir := filepath.Join("..", "mysql", "db")
	paths := []string{
		filepath.Join(sqlDir, "0_Schema.sql"),
		filepath.Join(sqlDir, "1_DummyEstateData.sql"),
		filepath.Join(sqlDir, "2_DummyChairData.sql"),
	}

	for _, p := range paths {
		sqlFile, _ := filepath.Abs(p)
		cmdStr := fmt.Sprintf("mysql -h %v -u %v -p%v -P %v %v < %v",
			mySQLConnectionData.Host,
			mySQLConnectionData.User,
			mySQLConnectionData.Password,
			mySQLConnectionData.Port,
			mySQLConnectionData.DBName,
			sqlFile,
		)
		if err := exec.Command("bash", "-c", cmdStr).Run(); err != nil {
			c.Logger().Errorf("Initialize script error : %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	return c.JSON(http.StatusOK, InitializeResponse{
		Language: "go",
	})
}

func getChairDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Echo().Logger.Errorf("Request parameter \"id\" parse error : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	chair := Chair{}
	query := `SELECT * FROM chair WHERE id = ?`
	err = db.GetContext(ctx, &chair, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Echo().Logger.Infof("requested id's chair not found : %v", id)
			return c.NoContent(http.StatusNotFound)
		}
		c.Echo().Logger.Errorf("Failed to get the chair from id : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	} else if chair.Stock <= 0 { // 0 になったときに消すようにしたのでもうヒットすることはなくなったはずだけど念のため
		c.Echo().Logger.Infof("requested id's chair is sold out : %v", id)
		return c.NoContent(http.StatusNotFound)
	}

	return c.JSON(http.StatusOK, chair)
}

func postChair(c echo.Context) error {
	header, err := c.FormFile("chairs")
	if err != nil {
		c.Logger().Errorf("failed to get form file: %v", err)
		return c.NoContent(http.StatusBadRequest)
	}
	f, err := header.Open()
	if err != nil {
		c.Logger().Errorf("failed to open form file: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		c.Logger().Errorf("failed to read csv: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	tx, err := db.Begin()
	if err != nil {
		c.Logger().Errorf("failed to begin tx: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer tx.Rollback()
	for _, row := range records {
		rm := RecordMapper{Record: row}
		id := rm.NextInt()
		name := rm.NextString()
		description := rm.NextString()
		thumbnail := rm.NextString()
		price := rm.NextInt()
		height := rm.NextInt()
		width := rm.NextInt()
		depth := rm.NextInt()
		color := rm.NextString()
		features := rm.NextString()
		kind := rm.NextString()
		popularity := rm.NextInt()
		stock := rm.NextInt()
		if err := rm.Err(); err != nil {
			c.Logger().Errorf("failed to read record: %v", err)
			return c.NoContent(http.StatusBadRequest)
		}
		_, err := tx.Exec("INSERT INTO chair(id, name, description, thumbnail, price, height, width, depth, color, features, kind, popularity, stock) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", id, name, description, thumbnail, price, height, width, depth, color, features, kind, popularity, stock)
		if err != nil {
			c.Logger().Errorf("failed to insert chair: %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}
	if err := tx.Commit(); err != nil {
		c.Logger().Errorf("failed to commit tx: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.NoContent(http.StatusCreated)
}

func searchChairs(c echo.Context) error {
	ctx := c.Request().Context()
	conditions := make([]string, 0)
	params := make([]interface{}, 0)

	if c.QueryParam("priceRangeId") != "" {
		chairPrice, err := getRange(chairSearchCondition.Price, c.QueryParam("priceRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("priceRangeID invalid, %v : %v", c.QueryParam("priceRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}

		if chairPrice.Min != -1 {
			conditions = append(conditions, "price >= ?")
			params = append(params, chairPrice.Min)
		}
		if chairPrice.Max != -1 {
			conditions = append(conditions, "price < ?")
			params = append(params, chairPrice.Max)
		}
	}

	if c.QueryParam("heightRangeId") != "" {
		chairHeight, err := getRange(chairSearchCondition.Height, c.QueryParam("heightRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("heightRangeIf invalid, %v : %v", c.QueryParam("heightRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}

		if chairHeight.Min != -1 {
			conditions = append(conditions, "height >= ?")
			params = append(params, chairHeight.Min)
		}
		if chairHeight.Max != -1 {
			conditions = append(conditions, "height < ?")
			params = append(params, chairHeight.Max)
		}
	}

	if c.QueryParam("widthRangeId") != "" {
		chairWidth, err := getRange(chairSearchCondition.Width, c.QueryParam("widthRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("widthRangeID invalid, %v : %v", c.QueryParam("widthRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}

		if chairWidth.Min != -1 {
			conditions = append(conditions, "width >= ?")
			params = append(params, chairWidth.Min)
		}
		if chairWidth.Max != -1 {
			conditions = append(conditions, "width < ?")
			params = append(params, chairWidth.Max)
		}
	}

	if c.QueryParam("depthRangeId") != "" {
		chairDepth, err := getRange(chairSearchCondition.Depth, c.QueryParam("depthRangeId"))
		if err != nil {
			c.Echo().Logger.Infof("depthRangeId invalid, %v : %v", c.QueryParam("depthRangeId"), err)
			return c.NoContent(http.StatusBadRequest)
		}

		if chairDepth.Min != -1 {
			conditions = append(conditions, "depth >= ?")
			params = append(params, chairDepth.Min)
		}
		if chairDepth.Max != -1 {
			conditions = append(conditions, "depth < ?")
			params = append(params, chairDepth.Max)
		}
	}

	if c.QueryParam("kind") != "" {
		conditions = append(conditions, "kind = ?")
		params = append(params, c.QueryParam("kind"))
	}

	if c.QueryParam("color") != "" {
		conditions = append(conditions, "color = ?")
		params = append(params, c.QueryParam("color"))
	}

	if c.QueryParam("features") != "" {
		for _, f := range strings.Split(c.QueryParam("features"), ",") {
			conditions = append(conditions, "features LIKE CONCAT('%', ?, '%')")
			params = append(params, f)
		}
	}

	if len(conditions) == 0 {
		c.Echo().Logger.Infof("Search condition not found")
		return c.NoContent(http.StatusBadRequest)
	}

	// もう stock が 0 のは残ってない
	// conditions = append(conditions, "stock > 0")

	page, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil {
		c.Logger().Infof("Invalid format page parameter : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	perPage, err := strconv.Atoi(c.QueryParam("perPage"))
	if err != nil {
		c.Logger().Infof("Invalid format perPage parameter : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	searchQuery := "SELECT * FROM chair WHERE "
	countQuery := "SELECT COUNT(*) FROM chair WHERE "
	searchCondition := strings.Join(conditions, " AND ")
	limitOffset := " ORDER BY popularity DESC, id ASC LIMIT ? OFFSET ?"

	var res ChairSearchResponse
	err = db.GetContext(ctx, &res.Count, countQuery+searchCondition, params...)
	if err != nil {
		c.Logger().Errorf("searchChairs DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	chairs := []Chair{}
	params = append(params, perPage, page*perPage)
	err = db.SelectContext(ctx, &chairs, searchQuery+searchCondition+limitOffset, params...)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusOK, ChairSearchResponse{Count: 0, Chairs: []Chair{}})
		}
		c.Logger().Errorf("searchChairs DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	res.Chairs = chairs

	return c.JSON(http.StatusOK, res)
}

func buyChair(c echo.Context) error {
	ctx := c.Request().Context()
	m := echo.Map{}
	if err := c.Bind(&m); err != nil {
		c.Echo().Logger.Infof("post buy chair failed : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	_, ok := m["email"].(string)
	if !ok {
		c.Echo().Logger.Info("post buy chair failed : email not found in request body")
		return c.NoContent(http.StatusBadRequest)
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Echo().Logger.Infof("post buy chair failed : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	tx, err := db.Beginx()
	if err != nil {
		c.Echo().Logger.Errorf("failed to create transaction : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer tx.Rollback()

	var chair Chair
	err = tx.QueryRowxContext(ctx, "SELECT * FROM chair WHERE id = ? AND stock > 0 FOR UPDATE", id).StructScan(&chair)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Echo().Logger.Infof("buyChair chair id \"%v\" not found", id)
			return c.NoContent(http.StatusNotFound)
		}
		c.Echo().Logger.Errorf("DB Execution Error: on getting a chair by id : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	// 最後のひとつだったら chair を消します
	if chair.Stock == 1 {
		_, err = tx.ExecContext(ctx, "DELETE FROM chair WHERE id = ?", id)
		if err != nil {
			c.Echo().Logger.Errorf("chair stock delete failed : %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE chair SET stock = stock - 1 WHERE id = ?", id)
		if err != nil {
			c.Echo().Logger.Errorf("chair stock update failed : %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	err = tx.Commit()
	if err != nil {
		c.Echo().Logger.Errorf("transaction commit error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}

func getChairSearchCondition(c echo.Context) error {
	return c.JSON(http.StatusOK, chairSearchCondition)
}

func getLowPricedChair(c echo.Context) error {
	ctx := c.Request().Context()
	var chairs []Chair
	query := `SELECT * FROM chair ORDER BY price ASC, id ASC LIMIT ?`
	err := db.SelectContext(ctx, &chairs, query, Limit)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Logger().Error("getLowPricedChair not found")
			return c.JSON(http.StatusOK, ChairListResponse{[]Chair{}})
		}
		c.Logger().Errorf("getLowPricedChair DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, ChairListResponse{Chairs: chairs})
}

func getEstateDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Echo().Logger.Infof("Request parameter \"id\" parse error : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	var estate Estate
	err = db.GetContext(ctx, &estate, "SELECT * FROM estate WHERE id = ?", id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Echo().Logger.Infof("getEstateDetail estate id %v not found", id)
			return c.NoContent(http.StatusNotFound)
		}
		c.Echo().Logger.Errorf("Database Execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, estate)
}

func getRange(cond RangeCondition, rangeID string) (*Range, error) {
	RangeIndex, err := strconv.Atoi(rangeID)
	if err != nil {
		return nil, err
	}

	if RangeIndex < 0 || len(cond.Ranges) <= RangeIndex {
		return nil, fmt.Errorf("Unexpected Range ID")
	}

	return cond.Ranges[RangeIndex], nil
}

// verify からしか来ないので newrelic いれない
func postEstate(c echo.Context) error {
	header, err := c.FormFile("estates")
	if err != nil {
		c.Logger().Errorf("failed to get form file: %v", err)
		return c.NoContent(http.StatusBadRequest)
	}
	f, err := header.Open()
	if err != nil {
		c.Logger().Errorf("failed to open form file: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		c.Logger().Errorf("failed to read csv: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	tx, err := db.Begin()
	if err != nil {
		c.Logger().Errorf("failed to begin tx: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer tx.Rollback()
	for _, row := range records {
		rm := RecordMapper{Record: row}
		id := rm.NextInt()
		name := rm.NextString()
		description := rm.NextString()
		thumbnail := rm.NextString()
		address := rm.NextString()
		latitude := rm.NextFloat()
		longitude := rm.NextFloat()
		rent := rm.NextInt()
		doorHeight := rm.NextInt()
		doorWidth := rm.NextInt()
		features := rm.NextString()
		popularity := rm.NextInt()
		if err := rm.Err(); err != nil {
			c.Logger().Errorf("failed to read record: %v", err)
			return c.NoContent(http.StatusBadRequest)
		}
		_, err := tx.Exec("INSERT INTO estate(id, name, description, thumbnail, address, latitude, longitude, rent, door_height, door_width, features, popularity) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id, name, description, thumbnail, address, latitude, longitude, rent, doorHeight, doorWidth, features, popularity)
		if err != nil {
			c.Logger().Errorf("failed to insert estate: %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}
	if err := tx.Commit(); err != nil {
		c.Logger().Errorf("failed to commit tx: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	// estates が変わったら redis の cache は飛ばさないといけない
	_ = purgeEstateIDsFromRedis()
	return c.NoContent(http.StatusCreated)
}

func genCacheKey(doorHeightRangeID string, doorWidthRangeID string, rentRangeID string, features string) string {
	return strings.Join([]string{doorHeightRangeID, doorWidthRangeID, rentRangeID, features}, "_")
}

var errCacheNotHit = errors.New("cache not hit")

// getFromRedis は redis から取得する。
// redis になかった場合は errCacheNotHit が帰ります
func getEstateIDsFromRedis(key string, limit int64, offset int64) ([]int64, int64, error) {
	ctx := context.TODO()
	// 全体の長さ
	length, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, 0, errCacheNotHit
		}
		return nil, 0, err
	}
	if length == 0 {
		return nil, 0, errCacheNotHit
	}
	val, err := rdb.LRange(ctx, key, offset, offset+limit-1).Result()
	if err != nil {
		// length があったならこっちがないことはないはず...
		if err == redis.Nil {
			return nil, 0, errCacheNotHit
		}
		return nil, 0, err
	}
	res := make([]int64, len(val))
	for i, v := range val {
		intVal, _ := strconv.Atoi(v)
		res[i] = int64(intVal)
	}
	return res, length, nil
}

func putEstateIDsToRedis(key string, res []int64) error {
	ctx := context.TODO()
	if len(res) == 0 {
		return nil
	}
	// del と rpush を atomic に行う (書き込み競合しても長さが2倍になったりしないはず)
	pipe := rdb.Pipeline()
	pipe.Del(ctx, key)
	idsStrSlice := make([]interface{}, len(res))
	for i, v := range res {
		idsStrSlice[i] = fmt.Sprintf("%d", v)
	}
	pipe.RPush(ctx, key, idsStrSlice...)
	_, err := pipe.Exec(ctx)
	if err != nil {
		fmt.Println(err)
	}
	return err
}

// purgeFromRedis は入稿したときにキャッシュを全滅させる
func purgeEstateIDsFromRedis() error {
	ctx := context.TODO()
	return rdb.FlushAllAsync(ctx).Err()
}

// キャッシュに埋める用
func searchEstateIDsFromMysql(ctx context.Context, doorHeightRangeID string, doorWidthRangeID string, rentRangeID string, features string) ([]int64, error) {
	conditions, params, errStatusCode := makeEstateConditions(doorHeightRangeID, doorWidthRangeID, rentRangeID, features)
	if errStatusCode != 0 {
		return nil, errors.New("failed")
	}

	if len(conditions) == 0 {
		// c.Echo().Logger.Infof("searchEstates search condition not found")
		return nil, errors.New("failed")
	}

	searchQuery := "SELECT id FROM estate WHERE "
	searchCondition := strings.Join(conditions, " AND ")
	order := " ORDER BY popularity DESC, id ASC"

	var ids []int64
	err := db.SelectContext(ctx, &ids, searchQuery+searchCondition+order, params...)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func searchEstatesFromIDs(ctx context.Context, ids []int64) ([]Estate, error) {
	var estates []Estate
	arg := map[string]interface{}{
		"ids": ids,
	}
	// estate.popularity の index は必要そう
	query, args, _ := sqlx.Named(`SELECT * FROM estate WHERE id IN (:ids) ORDER BY popularity DESC, id ASC`, arg)
	query, args, _ = sqlx.In(query, args...)
	query = db.Rebind(query)
	err := db.SelectContext(ctx, &estates, query, args...)
	return estates, err
}

func searchEstatesWithCache(ctx context.Context, doorHeightRangeID string, doorWidthRangeID string, rentRangeID string, features string, limit int64, offset int64) ([]Estate, int64, int) {
	key := genCacheKey(doorHeightRangeID, doorWidthRangeID, rentRangeID, features)
	ids, count, err := getEstateIDsFromRedis(key, limit, offset)
	if err == errCacheNotHit {
		estates, count, errStatusCode := searchEstatesWithoutCache(ctx, doorHeightRangeID, doorWidthRangeID, rentRangeID, features, limit, offset)
		// 非同期で cache を更新する
		go func(key string) {
			ctx := context.TODO()
			ids, err := searchEstateIDsFromMysql(ctx, doorHeightRangeID, doorWidthRangeID, rentRangeID, features)
			if err != nil {
				fmt.Println(err)
			}
			putEstateIDsToRedis(key, ids)
		}(key)
		return estates, count, errStatusCode
	}
	if err != nil {
		return nil, 0, http.StatusInternalServerError
	}
	estates, err := searchEstatesFromIDs(ctx, ids)
	if err != nil {
		return nil, 0, http.StatusInternalServerError
	}
	return estates, count, 0
}

func searchEstatesWithoutCache(ctx context.Context, doorHeightRangeID string, doorWidthRangeID string, rentRangeID string, features string, limit int64, offset int64) ([]Estate, int64, int) {
	conditions, params, errStatusCode := makeEstateConditions(doorHeightRangeID, doorWidthRangeID, rentRangeID, features)
	if errStatusCode != 0 {
		return nil, 0, errStatusCode
	}

	if len(conditions) == 0 {
		// c.Echo().Logger.Infof("searchEstates search condition not found")
		return nil, 0, http.StatusBadRequest
	}

	searchQuery := "SELECT * FROM estate WHERE "
	countQuery := "SELECT COUNT(*) FROM estate WHERE "
	searchCondition := strings.Join(conditions, " AND ")
	limitOffset := " ORDER BY popularity DESC, id ASC LIMIT ? OFFSET ?"

	var count int64
	err := db.GetContext(ctx, &count, countQuery+searchCondition, params...)
	if err != nil {
		// c.Logger().Errorf("searchEstates DB execution error : %v", err)
		return nil, 0, http.StatusInternalServerError
	}

	estates := []Estate{}
	params = append(params, limit, offset)
	err = db.SelectContext(ctx, &estates, searchQuery+searchCondition+limitOffset, params...)
	if err != nil {
		if err == sql.ErrNoRows {
			return estates, 0, 0 // 200
		}
		// c.Logger().Errorf("searchEstates DB execution error : %v", err)
		return nil, 0, http.StatusInternalServerError
	}
	return estates, count, 0
}

func makeEstateConditions(doorHeightRangeID string, doorWidthRangeID string, rentRangeID string, features string) ([]string, []interface{}, int) {
	conditions := make([]string, 0)
	params := make([]interface{}, 0)

	if doorHeightRangeID != "" {
		doorHeight, err := getRange(estateSearchCondition.DoorHeight, doorHeightRangeID)
		if err != nil {
			// c.Echo().Logger.Infof("doorHeightRangeID invalid, %v : %v", doorHeightRangeId, err)
			return conditions, params, http.StatusBadRequest
		}

		if doorHeight.Min != -1 {
			conditions = append(conditions, "door_height >= ?")
			params = append(params, doorHeight.Min)
		}
		if doorHeight.Max != -1 {
			conditions = append(conditions, "door_height < ?")
			params = append(params, doorHeight.Max)
		}
	}

	if doorWidthRangeID != "" {
		doorWidth, err := getRange(estateSearchCondition.DoorWidth, doorWidthRangeID)
		if err != nil {
			// c.Echo().Logger.Infof("doorWidthRangeID invalid, %v : %v", c.QueryParam("doorWidthRangeId"), err)
			return conditions, params, http.StatusBadRequest
		}

		if doorWidth.Min != -1 {
			conditions = append(conditions, "door_width >= ?")
			params = append(params, doorWidth.Min)
		}
		if doorWidth.Max != -1 {
			conditions = append(conditions, "door_width < ?")
			params = append(params, doorWidth.Max)
		}
	}

	if rentRangeID != "" {
		estateRent, err := getRange(estateSearchCondition.Rent, rentRangeID)
		if err != nil {
			// c.Echo().Logger.Infof("rentRangeID invalid, %v : %v", c.QueryParam("rentRangeId"), err)
			return conditions, params, http.StatusBadRequest
		}

		if estateRent.Min != -1 {
			conditions = append(conditions, "rent >= ?")
			params = append(params, estateRent.Min)
		}
		if estateRent.Max != -1 {
			conditions = append(conditions, "rent < ?")
			params = append(params, estateRent.Max)
		}
	}

	if features != "" {
		for _, f := range strings.Split(features, ",") {
			conditions = append(conditions, "features like concat('%', ?, '%')")
			params = append(params, f)
		}
	}
	return conditions, params, 0
}

func searchEstates(c echo.Context) error {
	ctx := c.Request().Context()

	page, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil {
		c.Logger().Infof("Invalid format page parameter : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	perPage, err := strconv.Atoi(c.QueryParam("perPage"))
	if err != nil {
		c.Logger().Infof("Invalid format perPage parameter : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	limit := int64(perPage)
	offset := int64(page * perPage)
	estates, count, errStatusCode := searchEstatesWithCache(ctx, c.QueryParam("doorHeightRangeId"), c.QueryParam("doorWidthRangeId"), c.QueryParam("rentRangeId"), c.QueryParam("features"), limit, offset)

	if errStatusCode != 0 {
		return c.NoContent(errStatusCode)
	}

	res := EstateSearchResponse{
		Estates: estates,
		Count:   count,
	}

	return c.JSON(http.StatusOK, res)
}

func getLowPricedEstate(c echo.Context) error {
	ctx := c.Request().Context()
	estates := make([]Estate, 0, Limit)
	query := `SELECT * FROM estate ORDER BY rent ASC, id ASC LIMIT ?`
	err := db.SelectContext(ctx, &estates, query, Limit)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Logger().Error("getLowPricedEstate not found")
			return c.JSON(http.StatusOK, EstateListResponse{[]Estate{}})
		}
		c.Logger().Errorf("getLowPricedEstate DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, EstateListResponse{Estates: estates})
}

func searchRecommendedEstateWithChair(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Logger().Infof("Invalid format searchRecommendedEstateWithChair id : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	chair := Chair{}
	query := `SELECT * FROM chair WHERE id = ?`
	err = db.GetContext(ctx, &chair, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.Logger().Infof("Requested chair id \"%v\" not found", id)
			return c.NoContent(http.StatusBadRequest)
		}
		c.Logger().Errorf("Database execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	var estates []Estate
	lengths := []int64{chair.Width, chair.Height, chair.Depth}
	sort.Slice(lengths, func(i, j int) bool {
		return lengths[i] < lengths[j]
	})
	m1, m2 := lengths[0], lengths[1]

	query = `SELECT * FROM estate WHERE (door_width >= ? AND door_height >= ?) OR (door_width >= ? AND door_height >= ?) ORDER BY popularity DESC, id ASC LIMIT ?`
	err = db.SelectContext(ctx, &estates, query, m1, m2, m2, m1, Limit)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusOK, EstateListResponse{[]Estate{}})
		}
		c.Logger().Errorf("Database execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, EstateListResponse{Estates: estates})
}

func searchEstateNazotte(c echo.Context) error {
	ctx := c.Request().Context()
	coordinates := Coordinates{}
	err := c.Bind(&coordinates)
	if err != nil {
		c.Echo().Logger.Infof("post search estate nazotte failed : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	if len(coordinates.Coordinates) == 0 {
		return c.NoContent(http.StatusBadRequest)
	}

	b := coordinates.getBoundingBox()
	estatesInBoundingBox := []Estate{}
	query := `SELECT * FROM estate WHERE latitude <= ? AND latitude >= ? AND longitude <= ? AND longitude >= ? ORDER BY popularity DESC, id ASC`
	err = db.SelectContext(ctx, &estatesInBoundingBox, query, b.BottomRightCorner.Latitude, b.TopLeftCorner.Latitude, b.BottomRightCorner.Longitude, b.TopLeftCorner.Longitude)
	if err == sql.ErrNoRows {
		c.Echo().Logger.Infof("select * from estate where latitude ...", err)
		return c.JSON(http.StatusOK, EstateSearchResponse{Count: 0, Estates: []Estate{}})
	} else if err != nil {
		c.Echo().Logger.Errorf("database execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	estatesInPolygon := []Estate{}
	for _, estate := range estatesInBoundingBox {
		validatedEstate := Estate{}

		point := fmt.Sprintf("'POINT(%f %f)'", estate.Latitude, estate.Longitude)
		query := fmt.Sprintf(`SELECT * FROM estate WHERE id = ? AND ST_Contains(ST_PolygonFromText(%s), ST_GeomFromText(%s))`, coordinates.coordinatesToText(), point)
		err = db.GetContext(ctx, &validatedEstate, query, estate.ID)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			} else {
				c.Echo().Logger.Errorf("db access is failed on executing validate if estate is in polygon : %v", err)
				return c.NoContent(http.StatusInternalServerError)
			}
		} else {
			estatesInPolygon = append(estatesInPolygon, validatedEstate)
		}
		if len(estatesInPolygon) == NazotteLimit {
			break
		}
	}

	var re EstateSearchResponse
	re.Estates = []Estate{}
	if len(estatesInPolygon) > NazotteLimit {
		re.Estates = estatesInPolygon[:NazotteLimit]
	} else {
		re.Estates = estatesInPolygon
	}
	re.Count = int64(len(re.Estates))

	return c.JSON(http.StatusOK, re)
}

func postEstateRequestDocument(c echo.Context) error {
	ctx := c.Request().Context()
	m := echo.Map{}
	if err := c.Bind(&m); err != nil {
		c.Echo().Logger.Infof("post request document failed : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	_, ok := m["email"].(string)
	if !ok {
		c.Echo().Logger.Info("post request document failed : email not found in request body")
		return c.NoContent(http.StatusBadRequest)
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Echo().Logger.Infof("post request document failed : %v", err)
		return c.NoContent(http.StatusBadRequest)
	}

	estate := Estate{}
	query := `SELECT * FROM estate WHERE id = ?`
	err = db.GetContext(ctx, &estate, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.NoContent(http.StatusNotFound)
		}
		c.Logger().Errorf("postEstateRequestDocument DB execution error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}

func getEstateSearchCondition(c echo.Context) error {
	return c.JSON(http.StatusOK, estateSearchCondition)
}

func (cs Coordinates) getBoundingBox() BoundingBox {
	coordinates := cs.Coordinates
	boundingBox := BoundingBox{
		TopLeftCorner: Coordinate{
			Latitude: coordinates[0].Latitude, Longitude: coordinates[0].Longitude,
		},
		BottomRightCorner: Coordinate{
			Latitude: coordinates[0].Latitude, Longitude: coordinates[0].Longitude,
		},
	}
	for _, coordinate := range coordinates {
		if boundingBox.TopLeftCorner.Latitude > coordinate.Latitude {
			boundingBox.TopLeftCorner.Latitude = coordinate.Latitude
		}
		if boundingBox.TopLeftCorner.Longitude > coordinate.Longitude {
			boundingBox.TopLeftCorner.Longitude = coordinate.Longitude
		}

		if boundingBox.BottomRightCorner.Latitude < coordinate.Latitude {
			boundingBox.BottomRightCorner.Latitude = coordinate.Latitude
		}
		if boundingBox.BottomRightCorner.Longitude < coordinate.Longitude {
			boundingBox.BottomRightCorner.Longitude = coordinate.Longitude
		}
	}
	return boundingBox
}

func (cs Coordinates) coordinatesToText() string {
	points := make([]string, 0, len(cs.Coordinates))
	for _, c := range cs.Coordinates {
		points = append(points, fmt.Sprintf("%f %f", c.Latitude, c.Longitude))
	}
	return fmt.Sprintf("'POLYGON((%s))'", strings.Join(points, ","))
}
