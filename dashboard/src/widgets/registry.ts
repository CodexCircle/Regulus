import type { ComponentType } from "react";
import type { MessageKey } from "../i18n";

// Translator is the function useT() returns.
export type Translator = (key: MessageKey, params?: Record<string, string | number>) => string;

// widgetTitle and widgetDescription give the text of a widget in the language
// of the translator: its catalog text when it has a key, else its own text.
export const widgetTitle = (def: { title: string; titleKey?: MessageKey }, t: Translator) => (def.titleKey ? t(def.titleKey) : def.title);
export const widgetDescription = (def: { description: string; descriptionKey?: MessageKey }, t: Translator) =>
  def.descriptionKey ? t(def.descriptionKey) : def.description;

// Props every widget component receives.
export interface WidgetProps {
  // Unique id of this placed widget (the same widget type can be placed more
  // than once).
  instanceId: string;
  // Settings saved with the widget in the layout; the shape is up to the widget.
  config: Record<string, unknown>;
  // Saves new settings for this widget in the layout.
  setConfig: (config: Record<string, unknown>) => void;
}

// WidgetDefinition describes a kind of widget. Built-in widgets and, later,
// widgets shipped by plugins register themselves with registerWidget.
export interface WidgetDefinition {
  // Stable id, saved in layouts. Plugins should prefix it with their own id.
  id: string;
  // The English title and description. A widget that ships translations also
  // sets titleKey and descriptionKey, and the dashboard shows those in the
  // language of the page (see widgetTitle); otherwise these texts are shown
  // as they are.
  title: string;
  description: string;
  titleKey?: MessageKey;
  descriptionKey?: MessageKey;
  // Size on the 12-column grid when the widget is added.
  defaultSize: { w: number; h: number };
  minSize?: { w: number; h: number };
  // Whether a fresh dashboard includes it.
  inDefaultLayout?: boolean;
  component: ComponentType<WidgetProps>;
}

export class WidgetRegistry {
  private readonly widgets = new Map<string, WidgetDefinition>();

  // register adds a widget type. A second registration of the same id is a
  // programming error, so it throws.
  register(def: WidgetDefinition): void {
    if (this.widgets.has(def.id)) {
      throw new Error(`widget "${def.id}" is already registered`);
    }
    this.widgets.set(def.id, def);
  }

  get(id: string): WidgetDefinition | undefined {
    return this.widgets.get(id);
  }

  list(): WidgetDefinition[] {
    return [...this.widgets.values()];
  }
}
