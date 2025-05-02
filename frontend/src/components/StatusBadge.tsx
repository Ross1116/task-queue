import React from 'react';
import { TaskStatus } from '@/lib/apiClient';

interface StatusBadgeProps {
	status: TaskStatus['status'];
}
export const StatusBadge: React.FC<StatusBadgeProps> = ({ status }) => {
	let bgColor = 'bg-gray-100';
	let textColor = 'text-gray-800';
	switch (status?.toUpperCase()) {
		case 'PENDING':
			bgColor = 'bg-yellow-100';
			textColor = 'text-yellow-800';
			break;
		case 'RUNNING':
			bgColor = 'bg-blue-100';
			textColor = 'text-blue-800';
			break;
		case 'COMPLETED':
			bgColor = 'bg-green-100';
			textColor = 'text-green-800';
			break;
		case 'FAILED':
			bgColor = 'bg-red-100';
			textColor = 'text-red-800';
			break;
	}
	return (
		<span className={`px-3 py-1 inline-flex text-sm font-semibold rounded-full ${bgColor} ${textColor}`}>
			{status || 'UNKNOWN'}
		</span>
	);
};
