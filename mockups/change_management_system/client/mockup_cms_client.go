package mockup_itfc_manager

/*Change Management System Client
Mockup Client to imitate ServiceNow/JIRA  REST APIs Client

MockupCMITFCManager is using this class internaly to invoke actual CMS APIS
*/
type MockupCMSClient struct {
}

func GetMockupCMSClient() (MockupCMSClient, error) {
	cmsClient := MockupCMSClient{}
	return cmsClient, nil
}

func (mcmsClient MockupCMSClient) GetChangeRequestDetails(changeRequestID string) (map[string]interface{}, error) {
	data := make(map[string]interface{})
	data["Number"] = "MOCKUPCMTICKET123"
	return data, nil

}

func (mcmsClient MockupCMSClient) SearchChangeRequestDetails(cmEnvironment, isActive, description, tag string) ([]map[string]interface{}, error) {
	data := make(map[string]interface{})
	data["Number"] = "MOCKUPCMTICKET123"
	var datas []map[string]interface{}
	datas = append(datas, data)
	return datas, nil
}

func (mcmsClient MockupCMSClient) CreateChangeRequest(changeRequestInput map[string]interface{}) (bool, string, error) {

	return true, "MOCKUPCMTICKET123", nil
}

func (mcmsClient MockupCMSClient) UpdateChangeRequest(changeRequestNo, workNotes string) (bool, error) {
	return true, nil
}

func (mcmsClient MockupCMSClient) ReviewChangeRequest(changeTicketNo, workNotes string) (bool, error) {

	return true, nil
}

func (mcmsClient MockupCMSClient) CheckInChangeRequest(changeTicketNo, workNotes, state string) (bool, error) {
	return true, nil

}

func (mcmsClient MockupCMSClient) SignOffChangeRequest(changeRequestNo, workNotes, closeNotes, closeCode, state string) (bool, error) {
	return true, nil
}

func (mcmsClient MockupCMSClient) SignoffChangeTask(parentID, taskNo, workNotes, state string) (bool, error) {
	return true, nil

}
