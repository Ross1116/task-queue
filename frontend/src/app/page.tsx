"use client";

import React, { useState, useEffect, useRef, useCallback } from 'react';
import { TaskStatus } from '@/lib/apiClient';
import { TaskForm } from '@/components/TaskForm';
import { TaskStatusDisplay } from '@/components/TaskStatusDisplay';
import { submitTask, getTaskStatus } from '@/lib/apiClient';

interface TrackedTask {
  id: string;
  status: TaskStatus | null;
  isLoading: boolean;
  error: string | null;
  isActivePolling: boolean;
}
export default function HomePage() {
  const [trackedTasks, setTrackedTasks] = useState<Record<string, TrackedTask>>({});
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const intervalRef = useRef<NodeJS.Timeout | null>(null);
  const isMounted = useRef(true);

  const updateTaskState = useCallback((taskId: string, updates: Partial<TrackedTask>) => {
    setTrackedTasks(prevTasks => {
      if (!prevTasks[taskId]) return prevTasks;
      return {
        ...prevTasks,
        [taskId]: { ...prevTasks[taskId], ...updates },
      };
    });
  }, []);

  const fetchStatusForTask = useCallback(async (taskId: string) => {
    if (!taskId) return;
    updateTaskState(taskId, { isLoading: true, error: null });
    try {
      const statusData = await getTaskStatus(taskId);
      if (isMounted.current) {
        const upperStatus = statusData.status?.toUpperCase();
        const isTerminal = upperStatus === 'COMPLETED' || upperStatus === 'FAILED';
        updateTaskState(taskId, {
          status: statusData,
          isLoading: false,
          error: null,
          isActivePolling: !isTerminal,
        });
        if (isTerminal) {
          console.log(`Polling stopped for task ${taskId}: Terminal state reached (${statusData.status})`);
        }
      }
    } catch (err: any) {
      console.error(`Error fetching status for task ${taskId}:`, err);
      if (isMounted.current) {
        const isNotFound = err.message === 'Task not found';
        updateTaskState(taskId, {
          isLoading: false,
          error: err.message || "Failed to fetch status.",
          isActivePolling: !isNotFound,
        });
        if (isNotFound) {
          console.log(`Polling stopped for task ${taskId}: Task not found`);
        }
      }
    }
  }, [updateTaskState]);

  const handleTaskSubmit = async (input: string) => {
    setIsSubmitting(true);
    setSubmitError(null);
    try {
      const data = await submitTask(input);
      if (isMounted.current) {
        const newTaskId = data.task_id;
        setTrackedTasks(prevTasks => ({
          ...prevTasks,
          [newTaskId]: {
            id: newTaskId,
            status: null,
            isLoading: true,
            error: null,
            isActivePolling: true,
          }
        }));
        fetchStatusForTask(newTaskId);
      }
    } catch (err: any) {
      if (isMounted.current) {
        setSubmitError(err.message || "Failed to submit task.");
      }
    } finally {
      if (isMounted.current) {
        setIsSubmitting(false);
      }
    }
  };

  useEffect(() => {
    isMounted.current = true;
    const pollActiveTasks = () => {
      console.log("Polling tick...");
      const tasksToPoll = Object.values(trackedTasks).filter(task => task.isActivePolling && !task.isLoading);
      if (tasksToPoll.length > 0) {
        console.log(`Polling status for tasks: ${tasksToPoll.map(t => t.id).join(', ')}`);
        tasksToPoll.forEach(task => {
          fetchStatusForTask(task.id);
        });
      } else {
        console.log("No active tasks to poll.");
      }
    };
    intervalRef.current = setInterval(pollActiveTasks, 1000);
    return () => {
      isMounted.current = false;
      if (intervalRef.current) {
        console.log("Clearing polling interval on unmount.");
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [fetchStatusForTask, trackedTasks]);

  const sortedTaskIds = Object.values(trackedTasks)
    .sort((a, b) => {
      const timeA = a.status?.created_at ? new Date(a.status.created_at).getTime() : 0;
      const timeB = b.status?.created_at ? new Date(b.status.created_at).getTime() : 0;
      return timeB - timeA;
    })
    .map(task => task.id);

  return (
    <main className="flex min-h-screen flex-col items-center justify-start p-6 sm:p-12 md:p-24 bg-gray-50 font-sans">
      <div className="z-10 w-full max-w-2xl items-center justify-between text-sm lg:flex flex-col">
        <h1 className="text-3xl font-bold mb-8 text-gray-800">Task Queue Frontend</h1>
        {}
        <div className="w-full bg-white p-6 rounded-lg shadow-md mb-6">
          <TaskForm onSubmit={handleTaskSubmit} isSubmitting={isSubmitting} />
          {}
          {submitError && (
            <p className="text-red-600 mt-4 p-3 bg-red-50 border border-red-200 rounded" role="alert">
              Submission Error: {submitError}
            </p>
          )}
        </div>

        {}
        <div className="w-full space-y-4">
          <h2 className="text-xl font-semibold text-gray-700">Tracked Tasks</h2>
          {sortedTaskIds.length === 0 && (
            <p className="text-gray-500 italic">No tasks submitted yet.</p>
          )}
          {sortedTaskIds.map(taskId => {
            const task = trackedTasks[taskId];
            if (!task || (!task.status && !task.isLoading && !task.error)) {
              return null;
            }
            return (
              <div key={task.id}>
                {!task.status && task.isLoading && (
                  <div className="mt-4 p-4 border border-gray-200 rounded-md shadow-sm bg-white space-y-3 animate-pulse">
                    <div className="h-4 bg-gray-200 rounded w-1/4"></div>
                    <div className="h-4 bg-gray-200 rounded w-3/4"></div>
                    <div className="h-4 bg-gray-200 rounded w-1/2"></div>
                  </div>
                )}
                {!task.status && !task.isLoading && task.error && (
                  <div className="mt-4 p-4 border border-red-200 rounded-md shadow-sm bg-red-50 text-red-700">
                    <p>Error loading initial status for Task ID: {task.id}</p>
                    <p className="text-sm">{task.error}</p>
                  </div>
                )}
                {task.status && (
                  <TaskStatusDisplay
                    taskStatus={task.status}
                    isLoading={task.isLoading}
                    error={task.error}
                  />
                )}
              </div>
            );
          })}
        </div>
      </div>
    </main>
  );
}

