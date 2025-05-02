"use client";

import React, { useState, useEffect, useRef, useCallback } from 'react';
import { TaskStatus } from '@/lib/apiClient';
import { TaskForm } from '@/components/TaskForm';
import { TaskStatusDisplay } from '@/components/TaskStatusDisplay';
import { submitTask, getTaskStatus } from '@/lib/apiClient';

export default function HomePage() {
  const [currentTaskId, setCurrentTaskId] = useState<string | null>(null);
  const [taskStatus, setTaskStatus] = useState<TaskStatus | null>(null);
  const [isLoadingStatus, setIsLoadingStatus] = useState<boolean>(false);
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const intervalRef = useRef<NodeJS.Timeout | null>(null);
  const isMounted = useRef(true);

  const stopPolling = useCallback((taskId: string | null, reason: string) => {
    if (intervalRef.current) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
      console.log(`Polling stopped for task ${taskId || 'N/A'}: ${reason}`);
    }
  }, []);

  const fetchStatus = useCallback(async (taskId: string | null) => {
    if (!taskId) return;
    setIsLoadingStatus(true);
    try {
      const statusData = await getTaskStatus(taskId);
      if (isMounted.current) {
        setTaskStatus(statusData);
        setError(null);
        const upperStatus = statusData.status?.toUpperCase();
        if (upperStatus === 'COMPLETED' || upperStatus === 'FAILED') {
          stopPolling(taskId, `Terminal state reached (${statusData.status})`);
        }
      }
    } catch (err: any) {
      console.error("Error in fetchStatus:", err);
      if (isMounted.current) {
        setError(err.message || "Failed to fetch status.");
        if (err.message === 'Task not found') {
          stopPolling(taskId, 'Task not found');
        }
      }
    } finally {
      if (isMounted.current) {
        setIsLoadingStatus(false);
      }
    }
  }, [stopPolling]);

  const handleTaskSubmit = async (input: string) => {
    setIsSubmitting(true);
    setError(null);
    setTaskStatus(null);
    setCurrentTaskId(null);
    stopPolling(currentTaskId, "New task submitted");
    try {
      const data = await submitTask(input);
      if (isMounted.current) {
        setCurrentTaskId(data.task_id);
      }
    } catch (err: any) {
      if (isMounted.current) {
        setError(err.message || "Failed to submit task.");
      }
    } finally {
      if (isMounted.current) {
        setIsSubmitting(false);
      }
    }
  };

  useEffect(() => {
    isMounted.current = true;
    if (currentTaskId) {
      console.log(`Effect: Starting polling for task ${currentTaskId}...`);
      fetchStatus(currentTaskId);
      intervalRef.current = setInterval(() => {
        console.log(`Polling status for task ${currentTaskId}...`);
        fetchStatus(currentTaskId);
      }, 1000);
    } else {
      stopPolling(null, "Task ID cleared");
    }
    return () => {
      isMounted.current = false;
      stopPolling(currentTaskId, "Component unmounting");
    };
  }, [currentTaskId, fetchStatus, stopPolling]);

  return (
    <main className="flex min-h-screen flex-col items-center justify-start p-6 sm:p-12 md:p-24 bg-gray-50 font-sans">
      <div className="z-10 w-full max-w-2xl items-center justify-between text-sm lg:flex flex-col">
        <h1 className="text-3xl font-bold mb-8 text-gray-800">Task Queue Frontend</h1>
        <div className="w-full bg-white p-6 rounded-lg shadow-md mb-6">
          <TaskForm onSubmit={handleTaskSubmit} isSubmitting={isSubmitting} />
        </div>
        <div className="w-full">
          {}
          {error && !taskStatus && (
            <p className="text-red-600 mb-4 p-3 bg-red-50 border border-red-200 rounded" role="alert">
              Error: {error}
            </p>
          )}
          {}
          <TaskStatusDisplay
            taskStatus={taskStatus}
            isLoading={isLoadingStatus}
            error={error}
          />
        </div>
      </div>
    </main>
  );
}
