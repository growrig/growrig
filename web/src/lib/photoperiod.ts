// Client-side mirror of Grow Core's photoperiod logic (growcore/internal/domain
// LightSchedule). Used to draw light-ON bands on the timeline — including the
// projected future, since the schedule is deterministic from wall-clock time.
import type { LightAnchor, LightSchedule, StageLightDefaults } from './types';

export interface Interval {
	start: number; // epoch ms
	end: number; // epoch ms
}

const DAY = 86_400_000;

function clampHours(h: number): number {
	return Math.max(0, Math.min(24, h));
}

function parseHHMM(s: string | undefined): number | null {
	const m = /^(\d{1,2}):(\d{2})$/.exec((s ?? '').trim());
	if (!m) return null;
	const h = +m[1];
	const mm = +m[2];
	if (h < 0 || h > 23 || mm < 0 || mm > 59) return null;
	return h * 60 + mm;
}

/** Hours of light for the given stage under this schedule. */
export function effectiveOnHours(
	sched: LightSchedule,
	stage: string,
	defaults: StageLightDefaults
): number {
	if (sched.mode === 'custom') return clampHours(sched.onHours);
	const override = sched.stageOnHours?.[stage];
	if (override != null) return clampHours(override);
	return defaults[stage] ?? 18;
}

/** Light window for a stage, as minutes since local midnight. `off` may be <=
 *  `on` when the window wraps past midnight. Mirrors LightSchedule.Window. */
export interface Window {
	on: number;
	off: number;
}

/** The light-on window for `stage`, or null when the schedule never flips
 *  (always on, always off, or an unparseable anchor time). */
export function lightWindow(
	sched: LightSchedule | undefined,
	stage: string,
	defaults: StageLightDefaults
): Window | null {
	if (!sched) return null;
	const hours = effectiveOnHours(sched, stage, defaults);
	if (hours <= 0 || hours >= 24) return null;
	const anchorAt = parseHHMM(sched.lightsOnAt);
	if (anchorAt == null) return null;
	const span = Math.round(hours * 60);
	// 'center' is the default: an absent anchor centres the day.
	let on = anchorAt;
	if (sched.anchor === 'end') on -= span;
	else if (sched.anchor !== 'start') on -= Math.trunc(span / 2);
	on = ((on % 1440) + 1440) % 1440;
	return { on, off: (on + span) % 1440 };
}

/** "06:00" for minutes-since-midnight. */
export function fmtMinutes(mins: number): string {
	const m = ((Math.round(mins) % 1440) + 1440) % 1440;
	return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

/** "18/6" — the light/dark split, trimming a trailing ".0". */
export function ratioLabel(onHours: number): string {
	const trim = (h: number) => String(Math.round(h * 10) / 10);
	return `${trim(clampHours(onHours))}/${trim(24 - clampHours(onHours))}`;
}

/** "12/12 · 09:00–21:00" — the effective photoperiod and the wall-clock window
 *  it lands on for this stage. */
export function windowLabel(
	sched: LightSchedule | undefined,
	stage: string,
	defaults: StageLightDefaults
): string {
	if (!sched) return '—';
	const hours = effectiveOnHours(sched, stage, defaults);
	const ratio = ratioLabel(hours);
	if (hours <= 0) return `${ratio} · dark all day`;
	if (hours >= 24) return `${ratio} · lit all day`;
	const win = lightWindow(sched, stage, defaults);
	return win ? `${ratio} · ${fmtMinutes(win.on)}–${fmtMinutes(win.off)}` : ratio;
}

/** Minute-of-day the light comes on, for an anchor time + duration. Mirrors
 *  the domain's window maths so editors can preview and convert anchors. */
export function onMinuteFor(anchor: LightAnchor, anchorAt: string, onHours: number): number | null {
	const at = parseHHMM(anchorAt);
	if (at == null) return null;
	const span = Math.round(clampHours(onHours) * 60);
	let on = at;
	if (anchor === 'end') on -= span;
	else if (anchor !== 'start') on -= Math.trunc(span / 2);
	return ((on % 1440) + 1440) % 1440;
}

/** The anchor time that keeps a window starting at `onMinute` in place under
 *  `anchor`. Used when the grower switches anchor so the day does not jump. */
export function anchorTimeFor(anchor: LightAnchor, onMinute: number, onHours: number): string {
	const span = Math.round(clampHours(onHours) * 60);
	let t = onMinute;
	if (anchor === 'end') t += span;
	else if (anchor !== 'start') t += Math.trunc(span / 2);
	return fmtMinutes(t);
}

export interface Transition {
	at: number; // epoch ms of the next boundary
	on: boolean; // true if the light turns ON at that boundary, false if OFF
}

/** The next light on/off boundary after `now`, or null when the schedule never
 *  flips (mode off, or an always-on / always-off duration). */
export function nextTransition(
	sched: LightSchedule | undefined,
	stage: string,
	defaults: StageLightDefaults,
	now: number
): Transition | null {
	if (!sched || sched.mode === 'off') return null;
	const win = lightWindow(sched, stage, defaults);
	if (!win) return null;
	const { on: onAt, off: offAt } = win;

	const midnight = new Date(now);
	midnight.setHours(0, 0, 0, 0);
	const base = midnight.getTime();
	const occur = (minute: number) => {
		const cand = base + minute * 60_000;
		return cand <= now ? cand + DAY : cand;
	};
	const onNext = occur(onAt);
	const offNext = occur(offAt);
	return onNext < offNext ? { at: onNext, on: true } : { at: offNext, on: false };
}

/** Light-ON intervals intersecting [start, end], per the current schedule. */
export function lightIntervals(
	sched: LightSchedule | undefined,
	stage: string,
	defaults: StageLightDefaults,
	start: number,
	end: number
): Interval[] {
	if (!sched || sched.mode === 'off') return [];
	const hours = effectiveOnHours(sched, stage, defaults);
	if (hours <= 0) return [];
	if (hours >= 24) return [{ start, end }];
	const win = lightWindow(sched, stage, defaults);
	if (!win) return [];
	const onAt = win.on;
	const durMs = hours * 3_600_000;

	const out: Interval[] = [];
	// Walk local-midnight days across the window (padded so a wrapped window at
	// the edges is still captured).
	const first = new Date(start);
	first.setHours(0, 0, 0, 0);
	for (let day = first.getTime() - DAY; day <= end + DAY; day += DAY) {
		const onStart = day + onAt * 60_000;
		const s = Math.max(onStart, start);
		const e = Math.min(onStart + durMs, end);
		if (e > s) out.push({ start: s, end: e });
	}
	return out;
}
