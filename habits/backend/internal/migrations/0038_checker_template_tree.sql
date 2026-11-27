-- +goose Up
-- Шаблоны Checker с подгруппами: тело шаблона хранится деревом в JSONB
-- ({items:[{name,done}], subgroups:[{name,items,subgroups}]}). Плоские пункты из
-- checker_template_items переносятся в body.items; таблица items становится
-- неиспользуемой (оставлена для совместимости отката).
ALTER TABLE checker_templates ADD COLUMN body JSONB NOT NULL DEFAULT '{"items":[],"subgroups":[]}';

UPDATE checker_templates t SET body = jsonb_build_object(
    'items', COALESCE((
        SELECT jsonb_agg(jsonb_build_object('name', i.name, 'done', false) ORDER BY i.position, i.id)
        FROM checker_template_items i WHERE i.template_id = t.id), '[]'::jsonb),
    'subgroups', '[]'::jsonb);

-- +goose Down
ALTER TABLE checker_templates DROP COLUMN body;
