import { Button, DateField, DateRangePicker, Label, RangeCalendar, TimeField } from '@heroui/react';
import type { CalendarDateTime, DateValue } from '@internationalized/date';
import { parseDate, parseDateTime, toCalendarDateTime } from '@internationalized/date';
import { useMemo, useRef, useState } from 'react';
import { X } from 'lucide-react';

interface UsageDateRangeFilterProps {
  clearLabel?: string;
  className?: string;
  endDate?: string;
  endTimeLabel?: string;
  label: string;
  onChange: (startDate: string, endDate: string) => void;
  startDate?: string;
  startTimeLabel?: string;
}

type DateRangeValue = {
  end: DateValue;
  start: DateValue;
} | null;

function toDateValue(value?: string): DateValue | null {
  if (!value) return null;
  // 秒粒度输出为 "2026-08-31T14:30:05";旧值/预设可能是纯日期,归一到
  // CalendarDateTime(00:00:00) 以免同一 range 里混两种粒度。
  try {
    return parseDateTime(value);
  } catch {
    try {
      return toCalendarDateTime(parseDate(value));
    } catch {
      return null;
    }
  }
}

export function UsageDateRangeFilter({
  clearLabel = 'Clear',
  // 秒粒度下起止两段的自然宽度随语言差异很大(zh 约 397px、zh-HK 456px、
  // en 空态 497px:mm/dd/yyyy , --:--:-- AM 两组)。固定宽度不是裁英文就是
  // 让中文白占半行,所以桌面按内容自适应,窄屏才退回占满一行。
  className = 'w-full sm:w-auto',
  endDate,
  endTimeLabel = 'End time',
  label,
  onChange,
  startDate,
  startTimeLabel = 'Start time',
}: UsageDateRangeFilterProps) {
  const value = useMemo<DateRangeValue>(() => {
    const start = toDateValue(startDate);
    const end = toDateValue(endDate);
    return start && end ? { start, end } : null;
  }, [endDate, startDate]);

  // RangeCalendar 只能选到「日」,时分秒得靠弹层里这两个 TimeField。给它喂
  // CalendarDateTime 时 onChange 会把日期部分原样带回来(只换时刻),所以直接
  // toString() 就是后端要的 "2006-01-02T15:04:05"。
  const commitTime = (edge: 'end' | 'start', next: CalendarDateTime | null) => {
    if (!value || !next) return;
    const nextRange = { ...value, [edge]: next as DateValue };
    onChange(nextRange.start.toString(), nextRange.end.toString());
  };

  // 选完第二个日期 react-aria 会立刻收起弹层,可时分秒还没来得及调。受控
  // isOpen + 只挡掉「选日期引起的那一次关闭」:点外部/Esc 不会触发 onChange,
  // 所以照常关得掉。
  const [isOpen, setIsOpen] = useState(false);
  const keepOpenRef = useRef(false);

  return (
    <DateRangePicker
      aria-label={label}
      className={`ag-usage-date-range ${className}`}
      endName="endDate"
      granularity="second"
      hideTimeZone
      isOpen={isOpen}
      startName="startDate"
      value={value}
      onChange={(nextValue) => {
        keepOpenRef.current = isOpen;
        onChange(nextValue?.start?.toString() ?? '', nextValue?.end?.toString() ?? '');
      }}
      onOpenChange={(next) => {
        if (!next && keepOpenRef.current) {
          keepOpenRef.current = false;
          return;
        }
        setIsOpen(next);
      }}
    >
      <Label className="sr-only">{label}</Label>
      <DateField.Group fullWidth>
        <DateField.Input slot="start">
          {(segment) => <DateField.Segment segment={segment} />}
        </DateField.Input>
        <DateRangePicker.RangeSeparator />
        <DateField.Input slot="end">
          {(segment) => <DateField.Segment segment={segment} />}
        </DateField.Input>
        <DateField.Suffix>
          {value ? (
            <Button
              aria-label={clearLabel}
              className="ag-date-range-clear"
              isIconOnly
              size="sm"
              type="button"
              variant="ghost"
              onPress={() => onChange('', '')}
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          ) : null}
          {/* 有值时也要留着日历入口:否则选完日期就再也打不开弹层,时分秒只能靠
              键盘在输入框里敲。 */}
          <DateRangePicker.Trigger className="ag-date-range-trigger">
            <DateRangePicker.TriggerIndicator />
          </DateRangePicker.Trigger>
        </DateField.Suffix>
      </DateField.Group>
      <DateRangePicker.Popover>
        <RangeCalendar aria-label={label}>
          <RangeCalendar.Header>
            <RangeCalendar.YearPickerTrigger>
              <RangeCalendar.YearPickerTriggerHeading />
              <RangeCalendar.YearPickerTriggerIndicator />
            </RangeCalendar.YearPickerTrigger>
            <RangeCalendar.NavButton slot="previous" />
            <RangeCalendar.NavButton slot="next" />
          </RangeCalendar.Header>
          <RangeCalendar.Grid>
            <RangeCalendar.GridHeader>
              {(day) => <RangeCalendar.HeaderCell>{day}</RangeCalendar.HeaderCell>}
            </RangeCalendar.GridHeader>
            <RangeCalendar.GridBody>
              {(date) => <RangeCalendar.Cell date={date} />}
            </RangeCalendar.GridBody>
          </RangeCalendar.Grid>
          <RangeCalendar.YearPickerGrid>
            <RangeCalendar.YearPickerGridBody>
              {({ year }) => <RangeCalendar.YearPickerCell year={year} />}
            </RangeCalendar.YearPickerGridBody>
          </RangeCalendar.YearPickerGrid>
        </RangeCalendar>
        <div className="ag-date-range-times">
          <TimeField
            granularity="second"
            isDisabled={!value}
            value={(value?.start ?? null) as CalendarDateTime | null}
            onChange={(next) => commitTime('start', next)}
          >
            <Label>{startTimeLabel}</Label>
            <TimeField.Group fullWidth>
              <TimeField.Input>
                {(segment) => <TimeField.Segment segment={segment} />}
              </TimeField.Input>
            </TimeField.Group>
          </TimeField>
          <TimeField
            granularity="second"
            isDisabled={!value}
            value={(value?.end ?? null) as CalendarDateTime | null}
            onChange={(next) => commitTime('end', next)}
          >
            <Label>{endTimeLabel}</Label>
            <TimeField.Group fullWidth>
              <TimeField.Input>
                {(segment) => <TimeField.Segment segment={segment} />}
              </TimeField.Input>
            </TimeField.Group>
          </TimeField>
        </div>
      </DateRangePicker.Popover>
    </DateRangePicker>
  );
}
