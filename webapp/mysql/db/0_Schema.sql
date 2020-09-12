DROP DATABASE IF EXISTS isuumo;
CREATE DATABASE isuumo;

DROP TABLE IF EXISTS isuumo.estate;
DROP TABLE IF EXISTS isuumo.chair;

CREATE TABLE isuumo.estate
(
    id          INTEGER             NOT NULL PRIMARY KEY,
    name        VARCHAR(64)         NOT NULL,
    description VARCHAR(4096)       NOT NULL,
    thumbnail   VARCHAR(128)        NOT NULL,
    address     VARCHAR(128)        NOT NULL,
    latitude    DOUBLE PRECISION    NOT NULL,
    longitude   DOUBLE PRECISION    NOT NULL,
    rent        INTEGER             NOT NULL,
    door_height INTEGER             NOT NULL,
    door_width  INTEGER             NOT NULL,
    features    VARCHAR(64)         NOT NULL,
    popularity  INTEGER             NOT NULL
);

create index `idx_estate_door_width_height_popularity` on isuumo.estate (`door_width`, `door_height`, `popularity`);
create index `idx_estate_rent_id` on isuumo.estate (`rent`, `id`);
create index `idx_estate_rent_popularity_id` on isuumo.estate (`rent`, `popularity`, `id`);

CREATE TABLE isuumo.chair
(
    id          INTEGER         NOT NULL PRIMARY KEY,
    name        VARCHAR(64)     NOT NULL,
    description VARCHAR(4096)   NOT NULL,
    thumbnail   VARCHAR(128)    NOT NULL,
    price       INTEGER         NOT NULL,
    height      INTEGER         NOT NULL,
    width       INTEGER         NOT NULL,
    depth       INTEGER         NOT NULL,
    color       VARCHAR(64)     NOT NULL,
    features    VARCHAR(64)     NOT NULL,
    kind        VARCHAR(64)     NOT NULL,
    popularity  INTEGER         NOT NULL,
    stock       INTEGER         NOT NULL
);

create index `idx_chair_price_popularity` on isuumo.chair (`price`, `popularity`);
create index `idx_chair_price_id` on isuumo.chair (`price`, `id`);
create index `idx_chair_price` on isuumo.chair (`price`);

-- ドアと椅子の大きさを持っておく

CREATE TABLE isuumo.estate_metrics
(
    id INTEGER NOT NULL AUTO_INCREMENT,
    estate_id INTEGER NOT NULL,
    -- door_width^2 + door_height^2
    d INTEGER NOT NULL,

    PRIMARY KEY (id)
);

CREATE TABLE isuumo.chair_metrics
(
    id INTEGER NOT NULL AUTO_INCREMENT,
    chair_id INTEGER NOT NULL,
    -- width^2 + height^2
    dwh_p INTEGER NOT NULL,
    -- width^2 + depth^2
    dwd_p INTEGER NOT NULL,
    -- height^2 + depth^2
    dhd_p INTEGER NOT NULL,

    PRIMARY KEY (id)
);
