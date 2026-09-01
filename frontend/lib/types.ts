// Shared API response shapes. Kept in one place so a field added for one
// screen can't silently drift from every other screen that shows the same
// resource. Optional fields mark shapes that vary by API endpoint.

export type Shift = {
  id: number;
  date: string;
  startTime: string;
  endTime: string;
  departmentName: string;
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

export type Job = {
  id: number;
  name: string;
  departmentId: number;
  departmentName: string;
  locationId: number;
  locationName: string;
  optimalWorkers: number;
  currentWorkers: number;
  weeklyHours: number;
  schedules: JobSchedule[];
  holidays: JobHoliday[];
};

export type Department = {
  id: number;
  name: string;
  locationId: number;
  locationName: string;
  departmentCode?: string | null;
};
