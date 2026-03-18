-- ============================================================
-- Incident List Report Template (dynamic columns)
-- Columns are NOT hardcoded — pass them at runtime via
-- overrides.columns in the GenerateReport request body.
-- ============================================================

INSERT INTO report_templates (id, name, description, template, is_public, is_html, is_default, created_by_id)
VALUES (
    gen_random_uuid(),
    'Incident List Report',
    'Landscape incident list report with filters header and dynamic columns (bilingual Arabic/English support)',
    '{
      "page_settings": {
        "size": "A3",
        "orientation": "landscape",
        "margin_top": 16,
        "margin_right": 16,
        "margin_bottom": 16,
        "margin_left": 16,
        "width": 0,
        "height": 0
      },
      "header": {
        "enabled": true,
        "height": 30,
        "background": "#f4f6f8",
        "elements": [
          {
            "id": "text_header_title",
            "type": "dynamic_field",
            "position": {"x": 130, "y": 10, "anchor": "top-left", "relative": false},
            "size": {"width": 100, "height": 12, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
            "style": {"border_width": 0, "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 0, "padding_right": 0, "padding_bottom": 0, "padding_left": 0},
            "content": {"alignment": "center", "field": "title", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 14, "style": "normal", "weight": "bold"}},
            "locked": false, "visible": true, "z_index": 0
          }
        ],
        "show_on_all_pages": true
      },
      "footer": {
        "enabled": true,
        "height": 15,
        "background": "#f4f6f8",
        "elements": [],
        "show_page_number": true,
        "page_number_format": "Page {page}",
        "show_on_all_pages": true
      },
      "elements": [
        {
          "id": "text_lbl_from_date",
          "type": "text",
          "position": {"x": 0, "y": 5, "anchor": "top-left", "relative": false},
          "size": {"width": 35, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "bold"}, "text": "From Date", "truncate": false, "v_alignment": "middle", "word_wrap": false},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "dynamic_from_date",
          "type": "dynamic_field",
          "position": {"x": 36, "y": 5, "anchor": "top-left", "relative": false},
          "size": {"width": 60, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "field": "filters.startDate", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "normal"}},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "text_lbl_to_date",
          "type": "text",
          "position": {"x": 0, "y": 15, "anchor": "top-left", "relative": false},
          "size": {"width": 35, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "bold"}, "text": "To Date", "truncate": false, "v_alignment": "middle", "word_wrap": false},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "dynamic_to_date",
          "type": "dynamic_field",
          "position": {"x": 36, "y": 15, "anchor": "top-left", "relative": false},
          "size": {"width": 60, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "field": "filters.endDate", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "normal"}},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "text_lbl_total",
          "type": "text",
          "position": {"x": 0, "y": 25, "anchor": "top-left", "relative": false},
          "size": {"width": 35, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "bold"}, "text": "Total", "truncate": false, "v_alignment": "middle", "word_wrap": false},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "dynamic_total",
          "type": "dynamic_field",
          "position": {"x": 36, "y": 25, "anchor": "top-left", "relative": false},
          "size": {"width": 60, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "field": "filters.total", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "normal"}},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "text_lbl_classification",
          "type": "text",
          "position": {"x": 230, "y": 5, "anchor": "top-left", "relative": false},
          "size": {"width": 35, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "bold"}, "text": "Classification", "truncate": false, "v_alignment": "middle", "word_wrap": false},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "dynamic_classification",
          "type": "dynamic_field",
          "position": {"x": 266, "y": 5, "anchor": "top-left", "relative": false},
          "size": {"width": 60, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "field": "filters.classification", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "normal"}},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "text_lbl_location",
          "type": "text",
          "position": {"x": 230, "y": 15, "anchor": "top-left", "relative": false},
          "size": {"width": 35, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "bold"}, "text": "Location", "truncate": false, "v_alignment": "middle", "word_wrap": false},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "dynamic_location",
          "type": "dynamic_field",
          "position": {"x": 266, "y": 15, "anchor": "top-left", "relative": false},
          "size": {"width": 60, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "field": "filters.location", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "normal"}},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "text_lbl_status",
          "type": "text",
          "position": {"x": 230, "y": 25, "anchor": "top-left", "relative": false},
          "size": {"width": 35, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "bold"}, "text": "Incident Status", "truncate": false, "v_alignment": "middle", "word_wrap": false},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "dynamic_status",
          "type": "dynamic_field",
          "position": {"x": 266, "y": 25, "anchor": "top-left", "relative": false},
          "size": {"width": 60, "height": 8, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": false},
          "style": {"border_width": 1, "border_color": "#333333", "border_style": "solid", "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 2, "padding_right": 2, "padding_bottom": 2, "padding_left": 2},
          "content": {"alignment": "left", "field": "filters.status", "font": {"color": "#222222", "family": "Arial", "line_height": 1.5, "size": 10, "style": "normal", "weight": "normal"}},
          "locked": false, "visible": true, "z_index": 0
        },
        {
          "id": "table_incidents",
          "type": "table",
          "position": {"x": 0, "y": 40, "anchor": "top-left", "relative": false},
          "size": {"width": 360, "height": 0, "min_width": 0, "min_height": 0, "max_width": 0, "max_height": 0, "unit": "mm", "auto_height": true},
          "style": {"border_width": 0, "border_radius": 0, "shadow": false, "shadow_blur": 0, "shadow_offset_x": 0, "shadow_offset_y": 0, "opacity": 1, "padding_top": 0, "padding_right": 0, "padding_bottom": 0, "padding_left": 0},
          "content": {
            "data_source": "incidents",
            "show_header": true,
            "show_row_numbers": true,
            "alternate_rows": true,
            "max_rows": 0,
            "pagination": false,
            "rows_per_page": 0,
            "filters": [],
            "sorting": [],
            "header_style": {
              "background_color": "#1a1a2e",
              "text_color": "#ffffff",
              "border_width": 0.5,
              "border_color": "#cccccc",
              "padding": 3,
              "v_alignment": "middle",
              "font": {"family": "Arial", "size": 9, "weight": "bold", "style": "normal", "color": "#ffffff", "line_height": 1.4}
            },
            "row_style": {
              "background_color": "#ffffff",
              "text_color": "#222222",
              "border_width": 0.5,
              "border_color": "#dddddd",
              "padding": 2,
              "v_alignment": "middle",
              "font": {"family": "Arial", "size": 8, "weight": "normal", "style": "normal", "color": "#222222", "line_height": 1.4}
            },
            "alt_row_style": {
              "background_color": "#f5f5f5",
              "text_color": "#222222",
              "border_width": 0.5,
              "border_color": "#dddddd",
              "padding": 2,
              "v_alignment": "middle",
              "font": {"family": "Arial", "size": 8, "weight": "normal", "style": "normal", "color": "#222222", "line_height": 1.4}
            },
            "columns": []
          },
          "locked": false,
          "visible": true,
          "z_index": 1
        }
      ]
    }',
    true,
    false,
    false,
    (SELECT id FROM users LIMIT 1)
);

-- ============================================================
-- Example GenerateReport curl — pass columns directly in the
-- request body under "columns". The service applies them to
-- the table element at runtime (no hardcoded columns in DB).
-- ============================================================
--
-- curl -X POST http://localhost:8080/api/v1/admin/report-templates/generate \
--   -H "Content-Type: application/json" \
--   -H "Authorization: Bearer <token>" \
--   -d '{
--     "template_id": "<uuid-from-above>",
--     "data_source": "incidents",
--     "format": "pdf",
--     "filters": [
--       {"field": "created_at", "operator": "gte", "value": "2026-01-01T00:00:00Z"},
--       {"field": "created_at", "operator": "lte", "value": "2026-03-16T23:59:59Z"}
--     ],
--     "sorting": [{"field": "created_at", "direction": "desc"}],
    "columns": [
      {"field": "reference_number",    "label": "Ref #"},
      {"field": "title",               "label": "Title / العنوان"},
      {"field": "classification_name", "label": "Classification / التصنيف"},
      {"field": "location_name",       "label": "Location / الموقع"},
      {"field": "department_name",     "label": "Department / القسم"},
      {"field": "current_state_name",  "label": "Status / الحالة"},
      {"field": "priority",            "label": "Priority / الأولوية"},
      {"field": "reporter_first_name", "label": "Reporter / المبلغ"},
      {"field": "created_at",          "label": "Date / التاريخ"}
    ],
--     "overrides": {
--       "title": "Incident List Report / تقرير قائمة الحوادث"
--     }
--   }' --output incident_report.pdf
