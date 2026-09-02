// Shared API response shapes. Kept in one place so a field added for one
// screen can't silently drift from every other screen that shows the same
// resource. Optional fields mark shapes that vary by API endpoint.

export type Shift = {
  id: number;
  date: string;
  startTime: string;
  endTime: string;
  teamName: string;
  status?: string;
  assignedUserId?: number | null;
  assignedEmail?: string | null;
};

export type Request = {
  id: number;
  date: string;
  startTime: string;
  endTime: string;
  type: string;
  reason?: string | null;
  workqueueId?: number;
  email?: string;
  status?: string;
};

export type Preference = {
  id: number;
  dayOfWeek: number;
  startTime: string;
  endTime: string;
};

export type JobSchedule = {
  dayOfWeek: number;
  startTime: string;
  endTime: string;
  hours: number;
};

export type JobHoliday = {
  date: string;
  reason?: string | null;
};

// A job sits in a team, which sits in a department — responses carry both
// levels.
export type Job = {
  id: number;
  name: string;
  teamId: number;
  teamName: string;
  departmentId: number;
  departmentName: string;
  optimalWorkers: number;
  currentWorkers: number;
  weeklyHours: number;
  schedules: JobSchedule[];
  holidays: JobHoliday[];
};

// A department is a managed site (with an assigned manager); teams are the
// working groups inside it.
export type Department = {
  id: number;
  name: string;
  abbreviation?: string | null;
  address?: string | null;
  address2?: string | null;
  city?: string | null;
  state?: string | null;
  zip?: string | null;
  country?: string | null;
  managerId?: number | null;
};

export type Team = {
  id: number;
  name: string;
  teamCode?: string | null;
  departmentId: number;
  departmentName: string;
};

export type AuditEntry = {
  id: number;
  actorId?: number | null;
  actorEmail?: string | null;
  action: string;
  entityType: string;
  entityId?: number | null;
  teamName?: string | null;
  details?: Record<string, unknown> | null;
  createdAt: string;
};
