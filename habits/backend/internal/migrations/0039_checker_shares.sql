-- +goose Up
-- Совместный доступ к чек-листам: group_id — группа верхнего уровня владельца,
-- user_id — участник. Участник видит всё поддерево и может менять пункты.
CREATE TABLE checker_shares (
    group_id   BIGINT NOT NULL REFERENCES checker_groups(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX checker_shares_user_idx ON checker_shares (user_id);

-- +goose Down
DROP TABLE checker_shares;
