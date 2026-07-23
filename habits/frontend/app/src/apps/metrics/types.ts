export interface ChartType {
  code: string
  name: string
}

/** Компонент (серия) метрики: линия на графике / сегмент столбца. */
export interface ComponentDef {
  key: string
  label: string
  color: string
}

export interface ItemConfig {
  components?: ComponentDef[]
  line?: {
    nodeColor?: string // цвет узлов (по умолчанию цвет линии)
    fill?: boolean // градиент под линией
  }
  bars?: {
    width?: number // ширина столбика, px (viewBox)
    gap?: number // расстояние между столбиками
  }
  tubes?: {
    tubeColor?: string // цвет «фона» трубки
  }
  background?: {
    from?: string
    to?: string // если не задан — один цвет; если ничего — прозрачный
  }
}

export interface MetricItem {
  id: number
  name: string
  chart_type: string
  config: ItemConfig
  position: number
}

export interface MetricCategory {
  id: number
  name: string
  position: number
  items: MetricItem[]
}

export interface MetricValue {
  id: number
  at: string
  component: string
  value: number
}

/** Точка графика: значения компонентов с общим временем. */
export interface ChartPoint {
  at: string
  values: Record<string, number>
  ids: Record<string, number>
}

export function groupValues(values: MetricValue[]): ChartPoint[] {
  const byAt = new Map<string, ChartPoint>()
  for (const v of values) {
    let p = byAt.get(v.at)
    if (!p) {
      p = { at: v.at, values: {}, ids: {} }
      byAt.set(v.at, p)
    }
    p.values[v.component] = v.value
    p.ids[v.component] = v.id
  }
  return [...byAt.values()].sort((a, b) => a.at.localeCompare(b.at))
}

export const DEFAULT_COMPONENT: ComponentDef = { key: '', label: '', color: '#60a5fa' }

export function itemComponents(config: ItemConfig): ComponentDef[] {
  return config.components?.length ? config.components : [DEFAULT_COMPONENT]
}
