-- +goose Up
-- Именованные цвета трекера: набор «цвет + название» (название видно только
-- в настройках трекера). Когда набор задан, на карточке вместо одного кружка
-- показывается весь набор, и клик по кружку выбирает активный цвет.
--
-- Запись вида {"color":"#rrggbb","color2":"#rrggbb","name":"Бег"};
-- непустой color2 означает градиент. Само значение цвета отметки лежит в
-- tracker_marks.color как раньше — '#rrggbb' или пара '#rrggbb,#rrggbb':
-- отметка хранит цвет целиком и не зависит от набора, поэтому удаление
-- цвета из набора не перекрашивает историю.
ALTER TABLE tracker_categories
    ADD COLUMN palette JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE tracker_categories
    DROP COLUMN palette;
