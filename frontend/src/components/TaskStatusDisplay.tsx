import React from 'react';
import { TaskStatus } from '@/lib/apiClient';
import { StatusBadge } from './StatusBadge';

interface TaskStatusDisplayProps {
	taskStatus: TaskStatus | null;
	isLoading: boolean;
	error: string | null;
}

export const TaskStatusDisplay: React.FC<TaskStatusDisplayProps> = ({ taskStatus, isLoading, error }) => {
	if (isLoading && !taskStatus) {
		return <p className="text-gray-600 animate-pulse">Loading status...</p>;
	}
	if (error) {
		return <p className="text-red-600" role="alert">Error fetching status: {error}</p>;
	}
	if (!taskStatus) {
		return <p className="text-gray-500">Submit a task to see its status.</p>;
	}
	const formatDate = (dateString: string) => {
		try {
			return new Date(dateString).toLocaleString();
		} catch {
			return dateString;
		}
	}
	return (
		<div className="mt-6 p-4 border border-gray-200 rounded-md shadow-sm bg-white space-y-3">
			<h3 className="text-lg font-medium leading-6 text-gray-900">Task Status</h3>
			<div className="flex justify-between items-center">
				<span className="text-sm font-medium text-gray-500">Task ID:</span>
				<span className="text-sm text-gray-900 font-mono break-all">{taskStatus.task_id}</span>
			</div>
			<div className="flex justify-between items-center">
				<span className="text-sm font-medium text-gray-500">Current Status:</span>
				<StatusBadge status={taskStatus.status} />
			</div>
			{taskStatus.input && (
				<div className="flex justify-between items-start">
					<span className="text-sm font-medium text-gray-500 flex-shrink-0 mr-2">Input:</span>
					<span className="text-sm text-gray-800 break-words text-right">{taskStatus.input}</span>
				</div>
			)}
			{taskStatus.result?.Valid && (
				<div className="flex justify-between items-start">
					<span className="text-sm font-medium text-gray-500 flex-shrink-0 mr-2">Result:</span>
					<pre className="text-sm text-gray-800 bg-gray-50 p-2 rounded overflow-x-auto text-right w-full">{taskStatus.result.String}</pre>
				</div>
			)}
			{taskStatus.result && !taskStatus.result.Valid && (
				<div className="flex justify-between items-center">
					<span className="text-sm font-medium text-gray-500">Result:</span>
					<span className="text-sm text-gray-500 italic">N/A</span>
				</div>
			)}
			<div className="flex justify-between items-center text-xs text-gray-500 pt-2 border-t border-gray-100">
				<span>Created: {formatDate(taskStatus.created_at)}</span>
				<span>Updated: {formatDate(taskStatus.updated_at)}</span>
			</div>
			{isLoading && <p className="text-xs text-blue-500 text-right animate-pulse">Updating...</p>}
		</div>
	);
};
