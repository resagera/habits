-- +goose Up

-- Хранилище паролей: один зашифрованный на устройстве блоб на пользователя.
-- Сервер видит только шифротекст (PBKDF2 + AES-256-GCM, ключ — мастер-пароль,
-- который никогда не покидает устройство). version — оптимистичная блокировка
-- против перезаписи со старого устройства.
CREATE TABLE password_vaults (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    version BIGINT NOT NULL DEFAULT 1,
    blob TEXT NOT NULL CHECK (length(blob) <= 2097152),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Передача папки паролей другому пользователю: пакет зашифрован на
-- устройстве отправителя случайным ключом; получатель забирает и удаляет.
CREATE TABLE password_shares (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    from_user BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    to_user BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    folder_name TEXT NOT NULL CHECK (length(folder_name) BETWEEN 1 AND 200),
    payload TEXT NOT NULL CHECK (length(payload) <= 1048576),
    key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX password_shares_to_idx ON password_shares (to_user);

-- +goose Down
DROP TABLE password_shares;
DROP TABLE password_vaults;
