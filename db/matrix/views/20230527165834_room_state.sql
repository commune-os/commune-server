DROP INDEX IF EXISTS room_state_idx;
DROP MATERIALIZED VIEW IF EXISTS room_state;
DROP TRIGGER room_state_mv_trigger on current_state_events;
DROP FUNCTION room_state_mv_refresh();

CREATE MATERIALIZED VIEW IF NOT EXISTS room_state AS 
    SELECT DISTINCT ON (rooms.room_id) rooms.room_id, ra.room_alias, COALESCE(rt.type, 'chat') as type, n.name, t.topic, av.avatar, h.header
    FROM rooms
    LEFT JOIN (
        SELECT ej.json::jsonb->'content'->>'type' as type, cse.room_id FROM current_state_events as cse LEFT JOIN event_json as ej ON cse.event_id = ej.event_id WHERE cse.type='m.space.child.type'
    ) as rt ON rt.room_id = rooms.room_id
    LEFT JOIN (
        SELECT ej.json::jsonb->'content'->>'name' as name, cse.room_id FROM current_state_events as cse LEFT JOIN event_json as ej ON cse.event_id = ej.event_id WHERE cse.type='m.room.name'
    ) as n ON n.room_id = rooms.room_id
    LEFT JOIN (
        SELECT ej.json::jsonb->'content'->>'topic' as topic, cse.room_id FROM current_state_events as cse LEFT JOIN event_json as ej ON cse.event_id = ej.event_id WHERE cse.type='m.room.topic'
    ) as t ON t.room_id = rooms.room_id
    LEFT JOIN (
        SELECT ej.json::jsonb->'content'->>'url' as avatar, cse.room_id FROM current_state_events as cse LEFT JOIN event_json as ej ON cse.event_id = ej.event_id WHERE cse.type='m.room.avatar'
    ) as av ON av.room_id = rooms.room_id
    LEFT JOIN (
        SELECT ej.json::jsonb->'content'->>'url' as header, cse.room_id FROM current_state_events as cse LEFT JOIN event_json as ej ON cse.event_id = ej.event_id WHERE cse.type='m.room.header'
    ) as h ON h.room_id = rooms.room_id
    LEFT JOIN room_aliases ra ON ra.room_id = rooms.room_id
    GROUP BY rooms.room_id, ra.room_alias, rt.type, n.name, t.topic, av.avatar, h.header;

CREATE UNIQUE INDEX IF NOT EXISTS room_state_idx ON room_state (room_id);

CREATE OR REPLACE FUNCTION room_state_mv_refresh()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    REFRESH MATERIALIZED VIEW CONCURRENTLY room_state;
    RETURN NULL;
END;
$$;

CREATE TRIGGER room_state_mv_trigger 
AFTER INSERT 
ON current_state_events
EXECUTE FUNCTION room_state_mv_refresh();
