import React, { useState } from 'react';
import { Button } from './Button';

interface TaskFormProps {
	onSubmit: (input: string) => Promise<void>;
	isSubmitting: boolean;
}

export const TaskForm: React.FC<TaskFormProps> = ({ onSubmit, isSubmitting }) => {
	const [inputValue, setInputValue] = useState('');
	const [error, setError] = useState<string | null>(null);
	const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		setError(null);
		if (!inputValue.trim()) {
			setError("Input cannot be empty.");
			return;
		}
		try {
			await onSubmit(inputValue);
			setInputValue('');
		} catch (err: any) {
			setError(err.message || "An unexpected error occurred during submission.");
		}
	};
	return (
		<form onSubmit={handleSubmit} className="space-y-4">
			<div>
				<label htmlFor="taskInput" className="block text-sm font-medium text-gray-700 mb-1">
					Task Input
				</label>
				<textarea
					id="taskInput"
					name="taskInput"
					rows={3}
					className="shadow-sm focus:ring-blue-500 focus:border-blue-500 block w-full sm:text-sm border border-gray-300 rounded-md p-2"
					placeholder="Enter data to process..."
					value={inputValue}
					onChange={(e) => setInputValue(e.target.value)}
					disabled={isSubmitting}
				/>
			</div>
			{error && (
				<p className="text-sm text-red-600" role="alert">
					Error: {error}
				</p>
			)}
			<div>
				<Button type="submit" disabled={isSubmitting || !inputValue.trim()}>
					{isSubmitting ? 'Submitting...' : 'Submit Task'}
				</Button>
			</div>
		</form>
	);
};
