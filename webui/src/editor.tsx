import React from "react";
import type { FieldProps, WidgetProps } from "@rjsf/utils";

export const CRON_PRESETS = ["@hourly", "@daily", "@midnight", "@weekly", "@monthly", "@yearly"];

const TOP_LEVEL_ORDER = ["cron", "source", "destination", "metrics", "webhook", "log"];
const ADVANCED_SECTIONS = ["metrics", "webhook", "log"];

export function buildUiSchema(schema?: Record<string, unknown>): Record<string, unknown> {
  const properties = (schema?.properties ?? {}) as Record<string, unknown>;
  const order = [
    ...TOP_LEVEL_ORDER.filter(key => key in properties),
    ...Object.keys(properties).filter(key => !TOP_LEVEL_ORDER.includes(key)),
  ];
  const uiSchema: Record<string, unknown> = {};
  if (order.length > 0) uiSchema["ui:order"] = order;
  if ("cron" in properties) uiSchema.cron = { "ui:widget": "cron" };
  for (const key of ADVANCED_SECTIONS) {
    if (key in properties) uiSchema[key] = { "ui:field": "advanced" };
  }
  return uiSchema;
}

export function CronWidget(props: WidgetProps) {
  const id = props.id;
  return <div className="cron-widget">
    <input
      id={id}
      className="form-control"
      value={typeof props.value === "string" ? props.value : ""}
      disabled={props.disabled || props.readonly}
      placeholder={props.placeholder ?? "@daily"}
      list={`${id}-presets`}
      onChange={event => props.onChange(event.target.value === "" ? undefined : event.target.value)}
    />
    <datalist id={`${id}-presets`}>{CRON_PRESETS.map(preset => <option key={preset} value={preset} />)}</datalist>
    <small>Five-field cron expression (for example 0 3 * * *) or a preset such as @daily.</small>
  </div>;
}

export function AdvancedField(props: FieldProps) {
  const SchemaField = props.registry.fields.SchemaField;
  if (!SchemaField) return null;
  const { "ui:field": _unused, ...nestedUiSchema } = (props.uiSchema ?? {}) as Record<string, unknown>;
  return <details className="advanced-field">
    <summary>{props.title ?? props.name ?? "Advanced"}</summary>
    <SchemaField {...props} uiSchema={nestedUiSchema} />
  </details>;
}
